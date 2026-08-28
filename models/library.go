package models

import (
	"strings"

	"bookfinder-backend/database"
	"bookfinder-backend/types"
	"bookfinder-backend/utils/schema"

	"gorm.io/gorm"
)

// GetLibraries 分页查询图书馆，关键字匹配承担 searchname 角色的字段值。
// 返回前按注册表规范化 Info，使库中旧记录也带上新增字段的空值。
func GetLibraries(query *types.LibraryQuery) ([]types.Library, int64, error) {
	var (
		libraries []types.Library
		total     int64
	)

	db := database.GetDB().Model(&types.Library{})

	if query.Keyword != "" {
		db = applyKeyword(db, query.Keyword)
	}

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 新记录在前；同一时刻的记录再按 ID 定序，保证分页稳定
	offset := (query.Page - 1) * query.Size
	if err := db.Order("created_at DESC, id DESC").Offset(offset).Limit(query.Size).Find(&libraries).Error; err != nil {
		return nil, 0, err
	}

	for i := range libraries {
		libraries[i].Info = schema.Normalize(libraries[i].Info)
	}

	return libraries, total, nil
}

// ngramTokenSize MySQL 的 ngram 解析器切词长度（服务端默认值 2）。
//
// 短于它的关键词在全文索引里没有对应的 token，故须另走 LIKE。
// 与服务端的 ngram_token_size 不一致只影响「哪些词走哪条路」，不影响正确性：
// 配得偏小会让本可走索引的词退到 LIKE，偏大则让 MATCH 查不到而回退（见下）。
const ngramTokenSize = 2

// applyKeyword 按记录名匹配关键词。
//
// 走 search_name 生成列上的 ngram 全文索引，而非对 JSON 列做
// JSON_EXTRACT + LIKE '%kw%'：后者每行都要解析一遍 JSON，且前导通配符
// 让任何 B-tree 索引都用不上（实测 EXPLAIN 为全表扫），而这张表无界增长。
//
// 短关键词退回 LIKE：ngram 按固定长度切词，短于 token 尺寸的词在索引里
// 没有对应条目，用 MATCH 会得到空结果——那比慢更糟。这类查询仍是全表扫，
// 但单字搜索本就少见，且结果集通常很大、用户会继续补字。
func applyKeyword(db *gorm.DB, keyword string) *gorm.DB {
	// 关键词以字符数而非字节数计：一个汉字三字节，按字节算会把「大学」误判为长词
	if len([]rune(keyword)) < ngramTokenSize {
		return db.Where("search_name LIKE ?", "%"+keyword+"%")
	}

	// BOOLEAN MODE：不做相关度排序也不忽略高频词，语义最接近原先的「包含即匹配」。
	// 关键词作为参数传入，不拼进 SQL。
	return db.Where("MATCH(search_name) AGAINST(? IN BOOLEAN MODE)",
		quoteFullTextPhrase(keyword))
}

// quoteFullTextPhrase 把关键词包成 BOOLEAN MODE 的引号短语，使其按字面匹配。
//
// 不包的话，关键词里的 + - > < ( ) ~ * @ 会被当成操作符而非字面内容：
// 实测搜「测试-中心」返回空（`-` 是「排除」操作符），搜「A+B」也是空，
// 尽管库里确有这两条记录。
//
// 短语内部只需转义双引号与反斜杠——前者会提前结束短语，后者是转义符本身。
func quoteFullTextPhrase(keyword string) string {
	var builder strings.Builder
	builder.Grow(len(keyword) + 2)

	builder.WriteByte('"')
	for _, r := range keyword {
		if r == '"' || r == '\\' {
			builder.WriteByte('\\')
		}
		builder.WriteRune(r)
	}
	builder.WriteByte('"')

	return builder.String()
}

// GetLibraryByID 根据 ID 获取图书馆，Info 按注册表规范化后返回
func GetLibraryByID(id int) (*types.Library, error) {
	var library types.Library
	if err := database.GetDB().First(&library, id).Error; err != nil {
		return nil, err
	}
	library.Info = schema.Normalize(library.Info)
	return &library, nil
}

// CreateLibrary 创建图书馆，ID 由数据库自增分配
func CreateLibrary(library *types.Library) error {
	return database.GetDB().Create(library).Error
}

// UpdateLibrary 更新图书馆，只改 Info
func UpdateLibrary(library *types.Library) error {
	return database.GetDB().Model(&types.Library{}).
		Where("id = ?", library.ID).
		Update("info", library.Info).Error
}

// SetFieldStatus 只改写某个字段的状态。
// 用 JSON_SET 定点更新而非整块重写 info：后者在并发报告同一记录的不同字段时，
// 后写的会覆盖前写的，导致一次报告静默丢失。
// 字段名由注册表决定，无法参数化进 JSON 路径，故 schema.Validate 已禁掉其中的引号与反斜杠。
func SetFieldStatus(libraryID int, fieldName string, status types.LibraryStatus) error {
	path := `$."` + fieldName + `".status`
	return database.GetDB().Model(&types.Library{}).
		Where("id = ?", libraryID).
		Update("info", gorm.Expr("JSON_SET(info, ?, ?)", path, string(status))).Error
}

// DeleteLibrary 删除图书馆，连带清理其字段报告
func DeleteLibrary(id int) error {
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := DeleteLibraryReports(tx, id); err != nil {
			return err
		}
		return tx.Delete(&types.Library{}, id).Error
	})
}

// MigrateLibraryInfo 按新的字段声明重写全表 Info：新增的字段补空值，删除的字段剔除。
// 注册表一改就调用它，使库中已有记录立即与注册表对齐，无需人工迁移。
// 整批放在一个事务里，失败则全部回滚。
func MigrateLibraryInfo(fields []types.InfoField) (int, error) {
	var migrated int

	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		var libraries []types.Library
		if err := tx.Find(&libraries).Error; err != nil {
			return err
		}

		for _, library := range libraries {
			info := schema.NormalizeWith(fields, library.Info)

			if err := tx.Model(&types.Library{}).
				Where("id = ?", library.ID).
				Update("info", info).Error; err != nil {
				return err
			}
			migrated++
		}

		return nil
	})
	if err != nil {
		return 0, err
	}

	return migrated, nil
}

// CountLibraries 图书馆总数，供监控面板展示。
//
// 单独一个计数查询而非复用 GetLibraries：后者还要取一页数据并逐条规范化 Info，
// 而面板只要一个数字。
func CountLibraries() (int64, error) {
	var total int64
	if err := database.GetDB().Model(&types.Library{}).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

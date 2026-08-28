package utils

import (
	"strconv"

	"bookfinder-backend/utils/sysconfig"

	"github.com/gin-gonic/gin"
)

// Pagination 解析分页参数，越界与非法取值一律回落到配置里的默认值。
//
// 默认条数与上限来自系统配置（管理页保存后即时生效）：上限是防止单次响应体积
// 失控的闸门，与请求体上限同理——限流按次数计，不约束单次请求的体量。
func Pagination(c *gin.Context) (page, size int) {
	limits := sysconfig.Get().Pagination

	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}

	size, err = strconv.Atoi(c.Query("size"))
	if err != nil || size < 1 || size > limits.MaxSize {
		size = limits.DefaultSize
	}

	return page, size
}

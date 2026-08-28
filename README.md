# BookFinder

图书馆信息服务。任何人都可以查阅与补充馆藏信息，不需要注册账号——也正因如此，
限流与封禁是这个项目的主体部分之一。

前端构建产物内嵌进可执行文件，部署时只有一个二进制、一份 `.env` 和两个配置文件。

## 目录

- [它解决什么问题](#它解决什么问题)
- [技术栈与依赖](#技术栈与依赖)
- [快速开始](#快速开始)
- [配置](#配置)
- [三层数据存储](#三层数据存储)
- [字段注册表](#字段注册表)
- [限流与封禁](#限流与封禁)
- [告警外发](#告警外发)
- [接口一览](#接口一览)
- [部署](#部署)
- [开发](#开发)

## 它解决什么问题

图书馆的开放时间、地址、网站这类信息会过时，而维护它们的人力有限。本服务的做法是
让访问者直接补充与纠正：

- **匿名可写。** 不需要注册，凭服务端下发的访问者令牌识别身份。
- **过时由多人共同判定。** 单个人报告不改变任何状态；同一字段被足够多的不同人
  报告后才自动标记为过时。
- **字段可增删。** 要记录的信息随时间变化，故字段由一份外部注册表声明，
  改文件重启即生效，数据库里已有的记录无需迁移。

开放写入的代价是必须防滥用，这是限流与自动封禁存在的原因。

## 技术栈与依赖

| | |
|---|---|
| 后端 | Go 1.25.7 · Gin 1.12 · GORM 1.31 |
| 前端 | React 19 · antd 6 · Vite（产物内嵌） |
| 业务数据 | MySQL 8.0+ |
| 用户与封禁 | SQLite（本地文件） |
| 限流计数 | Redis |

**MySQL 必须 8.0 及以上**：图书馆搜索依赖 `ngram` 解析器的全文索引，那是 8.0 才有的。

**Redis 无版本要求**，也不需要持久化：计数以自然日为单位过期，丢失只影响当日配额。
Redis 不可用时限流整体放行（fail-open），服务照常读写。

**SQLite 走 CGO**（`mattn/go-sqlite3`）。这决定了交叉编译的方式，见[部署](#部署)。

## 快速开始

```bash
# 1. 准备 MySQL 与 Redis，创建数据库（表由程序自动迁移）
mysql -u root -p -e "CREATE DATABASE bookfinder CHARACTER SET utf8mb4"

# 2. 写配置：至少填 DB_PASSWORD 与 JWT_SECRET
cp .env.example .env
$EDITOR .env

# 3. 放置必需的配置文件（这两个不会自动创建）
mkdir -p data && cp data.example/*.json data/

# 3. 构建前端（产物会被嵌入二进制）
cd frontend && npm install && npm run build && cd ..

# 4. 运行
go run .          # 生产模式
go run . -debug   # 调试模式：日志同时打到控制台
```

首次启动会创建唯一管理员账户并**在控制台打印一次性密码**，请立即保存。
管理员登录入口在 `/bookfinder/<ADMIN_ENTRY_TOKEN>`，访问其他路径不会显示登录界面。

## 配置

配置分两处，边界是「谁有权改它」：

- **`.env`** —— 连接串与密钥。只有能登录服务器的人才能改。
- **`data/system_config.json`** —— 运行参数。管理页可改，多数项保存即生效。

### 必填

| 变量 | 说明 |
|---|---|
| `DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME` | MySQL 连接 |
| `JWT_SECRET` | 管理员令牌与访问者令牌共用的签名密钥。**留空则启动失败** |

访问者令牌必须可验真，否则「没带就发一个新的」会让按令牌计数的限流形同不存在——
不带 Cookie 的请求每次都是全新访问者、每次都拿满配额。

### 重要可选项

| 变量 | 默认 | 说明 |
|---|---|---|
| `TRUSTED_PROXIES` | `127.0.0.1,::1` | 可信反代地址。**见下方警告** |
| `ADMIN_ENTRY_TOKEN` | 空 | 管理员入口口令，留空则入口完全关闭 |
| `APP_HMAC_SECRET` | 空 | 安卓端请求签名密钥，留空则不采信客户端上报的设备标识 |
| `SERVER_PORT` | `8080` | 监听端口 |
| `REDIS_ADDR` | `127.0.0.1:6379` | 限流计数 |
| `APP_DB_PATH` | `./data/app.db` | SQLite 文件路径 |

> **`TRUSTED_PROXIES` 配错会让封禁形同虚设。**
>
> 只有来自这些地址的请求，其 `X-Forwarded-For` 才被采信。若采信任意来源，
> 被封者改一个请求头就能绕过封禁，更能凭伪造的来源把任意 IP 送进封禁名单。
>
> - 服务直接对外监听 → 显式留空，一律以 TCP 对端地址为准
> - 服务在 Nginx 之后 → 填反代地址（同机通常是 `127.0.0.1`）
>
> 填 `0.0.0.0/0` 或 `::/0` 会直接启动失败。

### 需要自带的文件

`data/system_config.json` 与 `data/app.db` 会自动创建，但这两个不会——
缺失时启动直接失败：

```
data/library_schema.json   字段注册表
data/rate_rules.json       限流与自动封禁规则
```

不自动创建是有意的：注册表决定这个服务记录什么信息，限流规则决定放多少流量进来。
两者都没有「适用于任何部署」的默认值，静默造一份出来只会让人以为配好了。

`data.example/` 里有可直接使用的模板（`data/` 本身不入版本库，因为它含运行时数据）：

```bash
mkdir -p data && cp data.example/*.json data/
```

## 三层数据存储

三处各存各的，边界由「丢了会怎样」划定：

| 存储 | 内容 | 丢失后果 |
|---|---|---|
| MySQL | 图书馆、过时报告、运行日志、操作日志 | 业务数据丢失，不可接受 |
| SQLite | 管理员、封禁主体与标识、申诉 | 封禁与账号丢失，不可接受 |
| Redis | 限流计数、访问统计 | 只影响当日配额与面板数字 |

封禁放 SQLite 而非 Redis：封禁是永久的，必须跨重启存活。而限流计数按天过期，
放 Redis 正合适。

封禁判定在**每个 API 请求**上执行，而 SQLite 只允许一个连接——若每个请求都查库，
封禁检查自己就成了最容易被打崩的瓶颈。故封禁名单全量驻留内存（记录以千计），
写操作后重建。

## 字段注册表

图书馆的信息字段不硬编码，由 `data/library_schema.json` 声明：

```json
{
  "fields": [
    { "name": "FullName", "label": "全称", "type": "string",
      "required": true, "summary": true, "role": "searchname" },
    { "name": "ShortName", "label": "简称", "type": "string",
      "required": false, "summary": true },
    { "name": "WebSite", "label": "网站", "type": "string",
      "required": false, "summary": false }
  ]
}
```

| 字段 | 说明 |
|---|---|
| `name` | 标识符，**只能增删不能改**。改名等于丢弃该字段的全部历史数据 |
| `label` | 显示名，随时可改，全表立即生效 |
| `type` | `string` `number` `bool` `object` `array` |
| `required` | 必填字段写入时不允许为空 |
| `summary` | 是否作为列显示在列表里，未勾选的收进每行「详情」 |
| `role` | `searchname` 表示这是记录名，搜索匹配的就是它 |

读写两侧都会按注册表规范化：未声明的字段剔除，缺失的补为对应类型的空值。
因此增删字段只改这个文件，库里的旧记录无需人工迁移。

**约束**：必须有且仅有一个字段承担 `searchname` 角色，它固定为 `FullName`，
且必须是 `string`、必填、且作为摘要——它是记录的身份，藏进详情会让表格只剩 ID。

管理页提供可视化编辑器，保存后热生效并自动补全已有记录。

## 限流与封禁

两者分工明确：**限流拦当日，封禁永久生效**。

### 限流

按访问者令牌计数（认证与申诉类按 IP，因为那时还没有令牌），每日重置。
六个类别各有独立配额与突发窗口：

| 类别 | 每日 | 突发 | 窗口 |
|---|---|---|---|
| `read` | 300 | 5 | 60s |
| `create` | 30 | 5 | 60s |
| `update` | 10 | 2 | 60s |
| `report` | 10 | 2 | 60s |
| `auth` | 20 | 5 | 300s |
| `appeal` | 10 | 3 | 300s |

未携带有效令牌的请求先扣「见习配额」（按 IP，20/日、5/60s），额度内放行并签发正式
令牌。于是正常用户的第一个请求就换到令牌，此后不受影响；坚持不带 Cookie 的脚本
被按 IP 的小额度拦死，清 Cookie 换配额不再免费。

管理员不受限流影响。

### 自动封禁

判据都是当日累计值，命中即永久封禁，只能由管理员解封或申诉受理解除。

| 规则 | 判据 |
|---|---|
| 配额溢出 | 当日尝试次数达配额的 3 倍——用满额度是正常使用，反复叩门不是 |
| 突发违规 | 累计 5 次撞上突发限制 |
| 重复报告 | 10 次疑似重复的过时报告 |
| 见习溢出 | 见习额度用尽后仍继续请求，达 5 倍 |
| 网段异常 | 网段总量达预算 5 倍，且流量集中在少数令牌上（≥80%） |

**封禁挂在「主体」上而非单个 IP。** 一次封禁会写入本次请求的全部标识：精确 IP、
访问者令牌、已验签的设备标识（IPv6 还会写 `/64` 网段）。任一标识命中即视为该主体，
所以换 IP、清 Cookie、或换用另一端都不足以脱身；而解封是按主体进行的，一次解除全部。

### 判据的粒度决定处置的粒度

这是贯穿整套设计的一条线：

- **IPv6 封 `/64`，IPv4 不封 `/24`。** 一个 `/64` 通常就是一个宽带用户，
  封它约等于封一个人；而一个 `/24` 背后可能是整个校园网出口。
- **网段异常优先只封那几个异常设备**（按访问者令牌），不连坐同段其他人。
  只有认不出异常设备时才退回网段级封禁。
- **IPv4 网段异常且认不出设备时不做任何处置**，只记一条待人工核查——
  封整段或封共享出口地址都会连坐无关的人。
- **回环地址不封禁地址标识**，只按令牌与设备封。回环不指向任何特定访问者：
  要么是本机脚本，要么是反代没透传真实来源——后者一封就是整站自封。

### 申诉

被封者可提交申诉，每个 IP 最多 3 次。申诉接口对被封者开放（否则申诉功能形同虚设），
但要求来源确实被封，故不会变成公开留言通道。受理即解封，并清掉触发封禁的当日计数——
不清的话解封后第一个请求就会重新命中、立刻再封。

## 告警外发

三类事件可推送给管理员：**自动封禁**、**网段流量异常**、**申诉请求**。
它们的共同点是「需要人知道，但服务自己已经处理完了」。

两条通道并行，都配好就都发——Telegram 在国内常需代理，邮件基本处处可达，
不必赌哪一条能出去。

```
TELEGRAM_BOT_TOKEN=123456789:AA...    # 向 @BotFather 申请
TELEGRAM_CHAT_ID=12345678             # 群组为负数
SMTP_PASSWORD=...                     # QQ 邮箱填「授权码」而非登录密码
```

凭据只走 `.env`，不进系统配置文件、也不回给前端——它们能代管理员发消息，
而系统配置以明文 JSON 落盘且接口会返回整份内容。邮件的服务器、端口、收件地址
在管理页配置（那些会变，密码不会）。

哪几类告警外发、经哪条通道，在管理页开关。

## 接口一览

`/api` 前缀下的中间件顺序即判定顺序：识别身份 → 校验客户端签名 → 下发/校验访问者
令牌 → 拦截被封禁者 → 采集流量指标 → 限流。

### 公开

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/libraries` | 列表，支持关键字搜索与分页 |
| GET | `/api/libraries/:id` | 详情 |
| POST | `/api/libraries` | 新增 |
| PUT | `/api/libraries/:id` | 修改 |
| POST | `/api/libraries/:id/fields/:field/report-outdated` | 报告某字段过时 |
| DELETE | `/api/libraries/:id/fields/:field/report-outdated` | 撤销自己的报告 |
| GET | `/api/library-schema` | 字段注册表，前端据此渲染表格与表单 |
| GET | `/api/rate-status` | 当前访问者的剩余配额 |
| GET | `/api/me` | 当前身份与权限 |
| GET | `/api/appeal/quota` | 申诉配额 |
| POST | `/api/appeal` | 提交申诉（仅被封者） |

### 管理员

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/verify-entry` | 校验入口口令 |
| POST | `/api/admin/login` | 登录 |
| GET | `/api/admin/dashboard` | 监控面板 |
| DELETE | `/api/libraries/:id` | 删除图书馆 |
| GET/PUT | `/api/admin/library-schema` | 字段注册表 |
| GET/PUT | `/api/admin/rate-rules` | 限流与自动封禁规则（热生效） |
| GET/PUT | `/api/admin/system/config` | 系统配置 |
| GET | `/api/admin/logs/{operations,app,meta}` | 日志查看 |
| GET/POST | `/api/admin/bans` | 封禁列表与手动封禁 |
| DELETE | `/api/admin/bans/:id` | 解封（按主体） |
| GET | `/api/admin/bans/ip/:ip/appeals` | 某 IP 的申诉详情 |
| PUT | `/api/admin/appeals/:id` | 处理申诉，受理则一并解封 |

权限用位模型（`utils/permissions.go`），只由角色推导、不落库，故管理员身份
无法通过改写数据库字段转让。管理员是唯一的，首次启动时创建。

## 部署

### 构建 Linux 二进制

SQLite 驱动走 CGO，**不能简单地 `CGO_ENABLED=0` 交叉编译**：那样能编译通过、
退出码 0，但产物里的 sqlite3 是个空壳，运行时一碰应用库就失败。

在 Linux 上（或 WSL）用本机 gcc 编译：

```bash
cd frontend && npm run build && cd ..     # 前端要先构建，它会被嵌入
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags="-s -w" -o build/bookfinder-linux-amd64 .
```

验证产物不是空壳：

```bash
strings build/bookfinder-linux-amd64 | grep -c "requires cgo to work"   # 应为 0
```

产物动态链接 glibc，故目标机需为 glibc 发行版（Ubuntu / Debian / CentOS）。
**Alpine 用 musl，需在 Alpine 容器内另行编译。**

### 部署清单

```
bookfinder-linux-amd64     # 二进制，前端已内嵌
.env                       # 配置
data/
  ├── library_schema.json  # 必须自带
  └── rate_rules.json      # 必须自带
```

`data/app.db` 与 `data/system_config.json` 首次启动自动创建。

### Nginx

建议用**独立子域名**指向服务。前端资源、API 基址、前端路由都以域名根路径为基准，
挂在子路径下需要同时改三处并重新构建。

```nginx
server {
    listen 443 ssl;
    server_name library.example.com;

    ssl_certificate     /etc/nginx/ssl/example.com.cer;
    ssl_certificate_key /etc/nginx/ssl/example.com.key;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

两处都要做，缺一不可：

1. Nginx 里的 `proxy_set_header X-Forwarded-For`（只写 `proxy_pass` 不会自动转发）
2. `.env` 里的 `TRUSTED_PROXIES=127.0.0.1`

缺任何一步，全部访问者会共用同一个来源：按 IP 的限流互相牵连（一个人用完额度，
所有人被拦），封禁也无法定位到具体访问者。配错时日志里会有明确的 ERROR 提示。

`proxy_pass` 末尾**不要**加斜杠——根路径部署不需要路径重写。

### 定期清理

日志表若不清理会一直增长：操作日志不受日志级别过滤（审计必须完整），
每次读取、报告、登录失败都写一条。内置的清理任务默认每日 03:30 执行，
操作日志保留 180 天、运行日志 30 天，均可在管理页调整。

## 开发

```bash
# 后端
go run . -debug          # 调试模式，日志同时打到控制台
go test ./...            # 全部测试
go vet ./...

# 前端（开发服务器，代理到 :8080）
cd frontend && npm run dev
```

部分测试需要本机 Redis（`services/dashboard`）与 MySQL（`models`）；
连不上时会自动跳过而非失败。

### 代码组织

```
api/
  handlers/      HTTP 处理函数
  middlewares/   身份、签名、令牌、封禁、指标、限流
  routes/        路由与中间件顺序
config/          .env 加载与校验
database/        三个数据源的初始化
logger/          异步日志，写 MySQL
models/          数据访问
services/
  dashboard/     监控指标采集
  notify/        Telegram 与邮件告警
  maintenance.go 定期清理
types/           数据结构与常量
utils/
  banlist/       内存封禁名单
  ratelimit/     限流与自动封禁判定
  netmask/       网段计算
  schema/        字段注册表
  sysconfig/     系统配置
```

三个热重载配置（限流规则、字段注册表、系统配置）都是同一套模式：
JSON 文件 + 内存副本 + Load/Validate/Get/Commit，落盘用「临时文件 + 改名」保证原子性，
写失败时回滚内存。

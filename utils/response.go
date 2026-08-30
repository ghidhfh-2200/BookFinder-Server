package utils

import (
	"net/http"

	"bookfinder-backend/types"

	"github.com/gin-gonic/gin"
)

// Response HTTP 响应结构
type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// PaginatedResponse 分页响应结构
type PaginatedResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
	Total   int64  `json:"total"`
	Page    int    `json:"page"`
	Size    int    `json:"size"`
}

// ResponseOK 成功响应
func ResponseOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: "成功",
		Data:    data,
	})
}

// ResponseSuccessWithCustomMessage 成功响应，带自定义消息
func ResponseSuccessWithCustomMessage(c *gin.Context, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    200,
		Message: message,
	})
}

// ResponsePaginated 分页成功响应
func ResponsePaginated(c *gin.Context, data any, total int64, page, size int) {
	c.JSON(http.StatusOK, PaginatedResponse{
		Code:    200,
		Message: "成功",
		Data:    data,
		Total:   total,
		Page:    page,
		Size:    size,
	})
}

// ResponseError 错误响应（业务错误码放在 body 中，HTTP 状态码始终为 200）
func ResponseError(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: message,
	})
}

// ResponseErrorWithData 错误响应，附带数据。
//
// 用于「操作被拒，但客户端需要知道现状」的场合：页面上的状态可能已经过期
// （别人先改了，而这个页面没刷新），只回一句错误的话，用户点一次失败一次，
// 却始终看不到为什么。附上服务端的当前状态，前端据此就地纠正显示。
func ResponseErrorWithData(c *gin.Context, code int, message string, data any) {
	c.JSON(http.StatusOK, Response{
		Code:    code,
		Message: message,
		Data:    data,
	})
}

// ResponseBanned 封禁响应。
//
// 前端据 data.banned 切到封禁页，故所有「因封禁被拒」的出口都必须走此函数：
// 若只回一句普通错误，触发封禁的那一刻用户只会看到报错，要等下次刷新才进封禁页。
//
// 不回传命中的具体标识：告诉被封者「你是因为设备标识被认出来的」，等于教他
// 下次该改哪一项。原因与时间足够其判断是否申诉。
func ResponseBanned(c *gin.Context, subject *types.BanSubject) {
	c.JSON(http.StatusOK, Response{
		Code:    http.StatusForbidden,
		Message: "当前来源已被封禁",
		Data: gin.H{
			"banned":     true,
			"reason":     subject.Reason,
			"detail":     subject.Detail,
			"source":     subject.Source,
			"created_at": subject.CreatedAt,
		},
	})
	c.Abort()
}

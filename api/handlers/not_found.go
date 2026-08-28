package handlers

import (
	"net/http"

	"bookfinder-backend/utils"

	"github.com/gin-gonic/gin"
)

// APINotFound 未匹配到的 API 请求
func APINotFound(c *gin.Context) {
	utils.ResponseError(c, http.StatusNotFound, "接口不存在: "+c.Request.URL.Path)
}

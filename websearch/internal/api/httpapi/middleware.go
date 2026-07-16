package httpapi

import (
	"github.com/gin-gonic/gin"
	"strings"
	"web-search-backend/runtime/httpx"
)

const requestIDKey = "request_id"

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		value := strings.TrimSpace(c.GetHeader("X-Request-Id"))
		if value == "" || len(value) > 128 {
			value = httpx.NewRequestID()
		}
		c.Set(requestIDKey, value)
		c.Header("X-Request-Id", value)
		c.Next()
	}
}
func requestIDValue(c *gin.Context) string {
	value, _ := c.Get(requestIDKey)
	result, _ := value.(string)
	return result
}
func cors(origins []string) gin.HandlerFunc {
	allowed := stringSet(origins)
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Headers", "Content-Type,X-Request-Id")
				c.Header("Access-Control-Allow-Methods", "POST,OPTIONS")
			}
		}
		if c.Request.Method == "OPTIONS" {
			c.Status(204)
			c.Abort()
			return
		}
		c.Next()
	}
}

package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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

// apiKeyAuth protects business routes with a Bearer token. It is a no-op when
// expected is empty, so local development without a configured key keeps working.
func apiKeyAuth(expected string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if expected == "" {
			c.Next()
			return
		}
		const prefix = "Bearer "
		header := c.GetHeader("Authorization")
		if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
			key := strings.TrimSpace(header[len(prefix):])
			if len(key) > 0 && subtle.ConstantTimeCompare([]byte(key), []byte(expected)) == 1 {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}
}

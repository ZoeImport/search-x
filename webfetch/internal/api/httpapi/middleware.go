package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
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
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
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

type strictDecoder struct{ decoder *json.Decoder }

func newStrictDecoder(reader io.Reader) *strictDecoder {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	return &strictDecoder{decoder: decoder}
}
func (d *strictDecoder) Decode(value any) error {
	if err := d.decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request must contain one JSON value")
	}
	return nil
}

func redactURL(raw string, storeQuery bool) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.User = nil
	if !storeQuery {
		parsed.RawQuery = ""
	}
	parsed.Fragment = ""
	return parsed.String()
}

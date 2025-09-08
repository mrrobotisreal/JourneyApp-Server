package middleware

import (
	"bytes"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestIDMiddleware ensures every request has a request_id available in headers and context
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		rid := c.Request.Header.Get("X-Request-ID")
		if rid == "" {
			rid = uuid.New().String()
		}
		c.Set("request_id", rid)
		c.Writer.Header().Set("X-Request-ID", rid)
		c.Next()
	}
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	if w.body != nil {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// RequestLoggingMiddleware logs request start/finish and any error responses with context fields
func RequestLoggingMiddleware(logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		blw := &bodyLogWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		c.Writer = blw

		// Log request start (minimal)
		logger.Infow("request started",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		)

		c.Next()

		durationMs := time.Since(start).Milliseconds()
		status := c.Writer.Status()
		uidVal, _ := c.Get("uid")
		uid := ""
		if s, ok := uidVal.(string); ok {
			uid = s
		}

		fields := []interface{}{
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"status", status,
			"duration_ms", durationMs,
			"client_ip", c.ClientIP(),
			"user_uid", uid,
		}

		if status >= 500 {
			logger.Errorw("request completed with server error", append(fields, "response", blw.body.String())...)
			return
		}
		if status >= 400 {
			logger.Warnw("request completed with client error", append(fields, "response", blw.body.String())...)
			return
		}
		logger.Infow("request completed", fields...)
	}
}

// RecoveryMiddleware converts panics to 500 responses and logs stack traces with context
func RecoveryMiddleware(logger *zap.SugaredLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Errorw("panic recovered",
					"request_id", c.GetString("request_id"),
					"panic", r,
					"stack", string(debug.Stack()),
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"query", c.Request.URL.RawQuery,
					"client_ip", c.ClientIP(),
				)
				c.AbortWithStatusJSON(500, gin.H{"error": "Internal server error", "request_id": c.GetString("request_id")})
			}
		}()
		c.Next()
	}
}



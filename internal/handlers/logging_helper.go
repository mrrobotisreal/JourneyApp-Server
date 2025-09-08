package handlers

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func requestContextFields(c *gin.Context) []interface{} {
	uidVal, _ := c.Get("uid")
	uid := ""
	if s, ok := uidVal.(string); ok {
		uid = s
	}
	return []interface{}{
		"request_id", c.GetString("request_id"),
		"method", c.Request.Method,
		"path", c.Request.URL.Path,
		"query", c.Request.URL.RawQuery,
		"client_ip", c.ClientIP(),
		"user_uid", uid,
	}
}

func logWithContext(logger *zap.SugaredLogger, c *gin.Context, level string, msg string, fields ...interface{}) {
	base := requestContextFields(c)
	all := append(base, fields...)
	switch level {
	case "debug":
		logger.Debugw(msg, all...)
	case "warn":
		logger.Warnw(msg, all...)
	case "error":
		logger.Errorw(msg, all...)
	default:
		logger.Infow(msg, all...)
	}
}

func (h *AuthHandler) logError(c *gin.Context, err error, msg string, fields ...interface{}) {
	if h.logger == nil { return }
	logWithContext(h.logger, c, "error", msg, append(fields, "error", err)...)
}

func (h *EntryHandler) logError(c *gin.Context, err error, msg string, fields ...interface{}) {
	if h.logger == nil { return }
	logWithContext(h.logger, c, "error", msg, append(fields, "error", err)...)
}

func (h *UsersHandler) logError(c *gin.Context, err error, msg string, fields ...interface{}) {
	if h.logger == nil { return }
	logWithContext(h.logger, c, "error", msg, append(fields, "error", err)...)
}

func (h *NotificationsHandler) logError(c *gin.Context, err error, msg string, fields ...interface{}) {
	if h.logger == nil { return }
	logWithContext(h.logger, c, "error", msg, append(fields, "error", err)...)
}



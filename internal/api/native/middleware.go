package native

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

type requestIDKey struct{}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

func requestIDs() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := rand.Text()
		ctx := context.WithValue(c.Request.Context(), requestIDKey{}, id)
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func requestLogging(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			logger.Info("HTTP request",
				"request_id", requestID(c.Request.Context()),
				"method", c.Request.Method,
				"path", c.Request.URL.Path,
				"status", c.Writer.Status(),
				"duration_ms", float64(time.Since(start))/float64(time.Millisecond),
				"response_bytes", max(c.Writer.Size(), 0),
			)
		}()
		c.Next()
	}
}

func panicRecovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("HTTP handler panic",
					"request_id", requestID(c.Request.Context()),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				if c.Writer.Written() {
					// A committed stream cannot be replaced with a JSON error.
					panic(http.ErrAbortHandler)
				}
				writeError(c, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		c.Next()
	}
}

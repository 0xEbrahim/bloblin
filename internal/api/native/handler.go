package native

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

type healthHandlers struct {
	isReady func() bool
}

func (h healthHandlers) health(c *gin.Context) {
	writeJSON(c, http.StatusOK, map[string]string{"status": "ok"})
}

func (h healthHandlers) readiness(c *gin.Context) {
	if !h.isReady() {
		writeError(c, http.StatusServiceUnavailable, "not_ready", "server is not ready")
		return
	}
	writeJSON(c, http.StatusOK, map[string]string{"status": "ready"})
}

func notFound(c *gin.Context) {
	writeError(c, http.StatusNotFound, "not_found", "route not found")
}

func methodNotAllowed(c *gin.Context) {
	writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

func writeJSON(c *gin.Context, status int, value any) {
	c.Header("Content-Type", "application/json")
	c.Header("Cache-Control", "no-store")
	c.Status(status)
	_ = json.NewEncoder(c.Writer).Encode(value)
}

func writeError(c *gin.Context, status int, code, message string) {
	c.Abort()
	body := errorBody{}
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = requestID(c.Request.Context())
	writeJSON(c, status, body)
}

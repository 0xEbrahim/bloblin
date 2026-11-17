package native

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewHandler keeps Gin inside the HTTP adapter; app owns the configured http.Server.
func NewHandler(logger *slog.Logger, ready func() bool) http.Handler {
	// Gin's debug output would mix plain text into the application's JSON logs.
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.HandleMethodNotAllowed = true
	_ = router.SetTrustedProxies(nil)
	router.Use(requestIDs(), requestLogging(logger), panicRecovery(logger))
	router.NoRoute(notFound)
	router.NoMethod(methodNotAllowed)

	registerRoutes(router, healthHandlers{isReady: ready})
	return router
}

func registerRoutes(router *gin.Engine, health healthHandlers) {
	router.GET("/healthz", health.health)
	router.GET("/readyz", health.readiness)

	// Register bucket, object, and upload routes on this group as each feature is implemented.
	router.Group("/v1")
}

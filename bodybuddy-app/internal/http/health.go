package http

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Pinger is implemented by dependencies that can be health-checked.
type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthHandlers holds liveness and readiness check logic.
type HealthHandlers struct {
	pingers []Pinger
}

// NewHealthHandlers creates handlers. Pass zero or more Pinger dependencies
// (e.g. DB pool, Redis client) that will be checked in /readyz.
func NewHealthHandlers(pingers ...Pinger) *HealthHandlers {
	return &HealthHandlers{pingers: pingers}
}

// Liveness handles GET /healthz.
// Always returns 200 — used by kubelet to decide whether to restart the container.
func (h *HealthHandlers) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// Readiness handles GET /readyz.
// Pings all registered dependencies. Returns 503 if any fail.
func (h *HealthHandlers) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	for _, p := range h.pingers {
		if err := p.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// RegisterHealthRoutes attaches /healthz and /readyz to the given engine.
func RegisterHealthRoutes(r *gin.Engine, h *HealthHandlers) {
	r.GET("/healthz", h.Liveness)
	r.GET("/readyz", h.Readiness)
}

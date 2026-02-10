package server

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/primal-host/avalauncher/internal/config"
)

func (s *Server) routes() {
	s.echo.GET("/health", s.handleHealth)
	s.echo.GET("/", s.handleDashboard)
	s.echo.GET("/api/status", s.handleStatus)
}

func (s *Server) handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"version": config.Version,
	})
}

func (s *Server) handleDashboard(c echo.Context) error {
	html := strings.ReplaceAll(dashboardHTML, "{{VERSION}}", config.Version)
	return c.HTML(http.StatusOK, html)
}

func (s *Server) handleStatus(c echo.Context) error {
	// Dashboard fetches without auth for card counts.
	// Authenticated requests get additional detail.
	authenticated := s.checkBearer(c)

	ctx := c.Request().Context()
	counts := map[string]int64{}
	tables := []string{"hosts", "nodes", "l1s", "events"}
	for _, t := range tables {
		var n int64
		// Table names are hardcoded constants, not user input.
		err := s.db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		counts[t] = n
	}

	resp := map[string]any{
		"version": config.Version,
		"counts":  counts,
	}
	if authenticated {
		resp["authenticated"] = true
	}
	return c.JSON(http.StatusOK, resp)
}

func (s *Server) checkBearer(c echo.Context) bool {
	if s.adminKey == "" {
		return false
	}
	auth := c.Request().Header.Get("Authorization")
	return strings.TrimPrefix(auth, "Bearer ") == s.adminKey
}

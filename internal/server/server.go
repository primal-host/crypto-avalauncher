package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/primal-host/avalauncher/internal/database"
)

// Server holds the Echo instance and dependencies.
type Server struct {
	echo     *echo.Echo
	db       *database.DB
	adminKey string
	addr     string
}

// New creates a configured Echo server.
func New(db *database.DB, addr, adminKey string) *Server {
	s := &Server{
		echo:     echo.New(),
		db:       db,
		adminKey: adminKey,
		addr:     addr,
	}
	s.echo.HideBanner = true
	s.echo.HidePort = true
	s.echo.Use(middleware.Recover())
	s.routes()
	return s
}

// Start begins listening. Blocks until the server stops.
func (s *Server) Start() error {
	slog.Info("server listening", "addr", s.addr)
	if err := s.echo.Start(s.addr); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

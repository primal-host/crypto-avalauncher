package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/primal-host/avalauncher/internal/config"
	"github.com/primal-host/avalauncher/internal/database"
	"github.com/primal-host/avalauncher/internal/server"
)

func main() {
	slog.Info("avalauncher starting", "version", config.Version)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	db, err := database.Open(ctx, cfg.DSN())
	cancel()
	if err != nil {
		slog.Error("database open failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database connected")

	srv := server.New(db, cfg.ListenAddr, cfg.AdminKey)

	go func() {
		if err := srv.Start(); err != nil {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down", "signal", sig.String())

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("stopped")
}

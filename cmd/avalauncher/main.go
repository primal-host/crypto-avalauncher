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
	"github.com/primal-host/avalauncher/internal/docker"
	"github.com/primal-host/avalauncher/internal/manager"
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

	// Docker client.
	dc, err := docker.New(cfg.DockerHost)
	if err != nil {
		slog.Error("docker client failed", "error", err)
		os.Exit(1)
	}
	defer dc.Close()

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	if err := dc.Ping(ctx); err != nil {
		cancel()
		slog.Error("docker ping failed", "error", err)
		os.Exit(1)
	}
	cancel()
	slog.Info("docker connected")

	// Health interval.
	healthInterval, err := time.ParseDuration(cfg.HealthInterval)
	if err != nil {
		slog.Error("invalid health interval", "error", err)
		os.Exit(1)
	}

	// Manager.
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	traefik := manager.TraefikConfig{
		Domain:  cfg.TraefikDomain,
		Network: cfg.TraefikNetwork,
		Auth:    cfg.TraefikAuth,
	}
	mgr, err := manager.New(ctx, dc, db.Pool, cfg.AvagoImage, cfg.AvagoNetwork, cfg.AvaxDockerNet, healthInterval, traefik)
	cancel()
	if err != nil {
		slog.Error("manager init failed", "error", err)
		os.Exit(1)
	}
	mgr.StartHealthPoller()
	mgr.StartHostPoller()

	srv := server.New(db, mgr, cfg.ListenAddr, cfg.AdminKey)

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

	mgr.StopHealthPoller()
	mgr.CloseClients()

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	slog.Info("stopped")
}

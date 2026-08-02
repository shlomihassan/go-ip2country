package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-ip2country/internal/config"
	"go-ip2country/internal/geo"
	_ "go-ip2country/internal/geo/csv"
	"go-ip2country/internal/httpapi"
	"go-ip2country/internal/ratelimit"
)

func main() {
	logLevel := new(slog.LevelVar)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	logLevel.Set(parseLevel(cfg.LogLevel))

	locator, err := geo.New(cfg.DatastoreType, cfg)
	if err != nil {
		logger.Error("failed to initialize datastore", "error", err)
		os.Exit(1)
	}

	globalLimiter := ratelimit.NewFixedWindow(cfg.RateLimitGlobalRPS, time.Second)
	perIPLimiter := ratelimit.NewFixedWindow(cfg.RateLimitPerIPRPS, time.Second)
	defer globalLimiter.Close()
	defer perIPLimiter.Close()

	router := httpapi.NewRouter(locator, globalLimiter, perIPLimiter)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           router,
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("starting server", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

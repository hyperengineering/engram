package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hyperengineering/engram/internal/api"
	"github.com/hyperengineering/engram/internal/config"
	"github.com/hyperengineering/engram/internal/embedding"
	"github.com/hyperengineering/engram/internal/store"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags: -ldflags "-X main.Version=1.0.0"
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "engram",
	Short: "Engram - Central Lore Service",
	RunE:  run,
}

func run(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Log.Level),
	}))
	slog.SetDefault(logger)

	// Initialize store
	db, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	// Initialize embedding service
	embedder := embedding.NewOpenAI(cfg.Embedding.APIKey, cfg.Embedding.Model)

	// Initialize API
	handler := api.NewHandlerWithLegacyStore(db, embedder, cfg.Auth.APIKey, Version)
	router := api.NewRouter(handler)

	// Build server address from port
	addr := fmt.Sprintf(":%d", cfg.Server.Port)

	// Start server
	server := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Starting Engram", "address", addr)
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("Server error", "error", err)
		}
	}()

	<-done
	slog.Info("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Server.ShutdownTimeout))
	defer cancel()

	return server.Shutdown(ctx)
}

func parseLogLevel(level string) slog.Level {
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

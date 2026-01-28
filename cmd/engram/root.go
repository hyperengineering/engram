package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	// 1. Signal handling - Architecture Decision #11
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// 2. Load configuration
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.Info("configuration loaded")

	// 3. Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.Log.Level),
	}))
	slog.SetDefault(logger)
	slog.Info("logger initialized", "level", cfg.Log.Level)

	// 4. Initialize store (migrations, WAL mode)
	db, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		return err
	}
	slog.Info("store initialized", "path", cfg.Database.Path)

	// 5. Initialize embedding service
	embedder := embedding.NewOpenAI(cfg.Embedding.APIKey, cfg.Embedding.Model)
	slog.Info("embedder initialized", "model", cfg.Embedding.Model)

	// 6. Initialize HTTP router
	handler := api.NewHandler(db, embedder, cfg.Auth.APIKey, Version)
	router := api.NewRouter(handler)
	slog.Info("router initialized")

	// 7. Configure HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	// 8. Worker lifecycle infrastructure
	var wg sync.WaitGroup
	// Future workers plug in here:
	// startWorker(ctx, &wg, "snapshot", snapshotWorker.Run)
	// startWorker(ctx, &wg, "decay", decayWorker.Run)
	// startWorker(ctx, &wg, "embedding-retry", embeddingRetryWorker.Run)

	// 9. Start HTTP server in goroutine
	go func() {
		slog.Info("server starting", "address", addr)
		// ErrServerClosed is the expected error when Shutdown() is called gracefully.
		// Any other error indicates an actual server failure that should trigger shutdown.
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel() // Trigger shutdown on server failure
		}
	}()

	// 10. Block until signal received
	<-ctx.Done()
	slog.Info("shutdown initiated")

	// 11. Graceful shutdown sequence
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.Server.ShutdownTimeout))
	defer shutdownCancel()

	// 11a. Stop HTTP server (drains in-flight requests)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// 11b. Wait for workers to complete
	wg.Wait()

	// 11c. Close store
	if err := db.Close(); err != nil {
		slog.Error("store close error", "error", err)
	}

	slog.Info("shutdown complete")
	return nil
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

// startWorker launches a background worker goroutine that respects context cancellation.
// Workers are tracked via WaitGroup for graceful shutdown.
func startWorker(ctx context.Context, wg *sync.WaitGroup, name string, fn func(ctx context.Context)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		slog.Info("worker started", "worker", name)
		fn(ctx)
		slog.Info("worker stopped", "worker", name)
	}()
}

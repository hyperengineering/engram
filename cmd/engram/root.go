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
	"github.com/hyperengineering/engram/internal/worker"
	"github.com/spf13/cobra"
)

// Version information set at build time via ldflags:
//
//	-X main.Version=1.0.0
//	-X main.Commit=abc1234
//	-X main.Date=2026-01-30T12:00:00Z
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "engram",
	Short: "Engram - Central Lore Service",
	RunE:  run,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("engram %s (commit: %s, built: %s)\n", Version, Commit, Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
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

	// 6. Configure store dependencies for deduplication
	db.SetDependencies(embedder, cfg)
	slog.Info("deduplication configured",
		"enabled", cfg.Deduplication.Enabled,
		"threshold", cfg.Deduplication.SimilarityThreshold)

	// 7. Initialize HTTP router
	handler := api.NewHandler(db, embedder, cfg.Auth.APIKey, Version)
	router := api.NewRouter(handler)
	slog.Info("router initialized")

	// 8. Configure HTTP server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	// 9. Worker lifecycle infrastructure
	var wg sync.WaitGroup

	// Initialize and start embedding retry worker
	embeddingRetryWorker := worker.NewEmbeddingRetryWorker(
		db,
		embedder,
		time.Duration(cfg.Worker.EmbeddingRetryInterval),
		cfg.Worker.EmbeddingRetryMaxAttempts,
		50, // batch size
	)
	startWorker(ctx, &wg, "embedding-retry", embeddingRetryWorker.Run)

	// Initialize and start snapshot generation worker
	snapshotWorker := worker.NewSnapshotGenerationWorker(
		db,
		time.Duration(cfg.Worker.SnapshotInterval),
	)
	startWorker(ctx, &wg, "snapshot-generation", snapshotWorker.Run)

	// Initialize and start confidence decay worker
	decayWorker := worker.NewConfidenceDecayWorker(
		db,
		time.Duration(cfg.Worker.DecayInterval),
		store.DefaultDecayAmount,
	)
	startWorker(ctx, &wg, "confidence-decay", decayWorker.Run)

	// 10. Start HTTP server in goroutine
	go func() {
		slog.Info("server starting", "address", addr)
		// ErrServerClosed is the expected error when Shutdown() is called gracefully.
		// Any other error indicates an actual server failure that should trigger shutdown.
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel() // Trigger shutdown on server failure
		}
	}()

	// 11. Block until signal received
	<-ctx.Done()
	slog.Info("shutdown initiated")

	// 12. Graceful shutdown sequence
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.Server.ShutdownTimeout))
	defer shutdownCancel()

	// 12a. Stop HTTP server (drains in-flight requests)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// 12b. Wait for workers to complete
	wg.Wait()

	// 12c. Close store
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

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
	"github.com/hyperengineering/engram/internal/multistore"
	"github.com/hyperengineering/engram/internal/snapshot"
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
	rootCmd.AddCommand(storeCmd)
}

func run(cmd *cobra.Command, args []string) error {
	// 1. Signal handling - Architecture Decision #11
	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// 2. Register domain plugins (must happen before any store operations)
	initPlugins()

	// 3. Load configuration
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

	// 7. Initialize multi-store support (Story 7.3)
	storeManager, err := multistore.NewStoreManager(cfg.Stores.RootPath)
	if err != nil {
		return fmt.Errorf("initialize store manager: %w", err)
	}
	defer storeManager.Close()
	slog.Info("store manager initialized", "root_path", cfg.Stores.RootPath)

	// 8. Initialize snapshot uploader (S3-compatible storage)
	uploader, err := snapshot.NewUploader(cfg.SnapshotStorage)
	if err != nil {
		return fmt.Errorf("initialize snapshot uploader: %w", err)
	}
	if cfg.SnapshotStorage.Bucket != "" {
		slog.Info("snapshot S3 upload enabled",
			"bucket", cfg.SnapshotStorage.Bucket,
			"region", cfg.SnapshotStorage.Region,
			"endpoint", cfg.SnapshotStorage.Endpoint,
		)
	}

	// 9. Initialize HTTP router
	handler := api.NewHandler(db, storeManager, embedder, uploader, cfg.Auth.APIKey, Version)
	router := api.NewRouter(handler, storeManager)
	slog.Info("router initialized")

	// 9. Configure HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout),
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout),
	}

	// 10. Worker lifecycle infrastructure
	var wg sync.WaitGroup

	// Initialize store manager adapters for multi-store workers
	storeAdapter := worker.NewStoreManagerAdapter(storeManager)
	decayAdapter := worker.NewDecayStoreManagerAdapter(storeManager)
	embeddingAdapter := worker.NewEmbeddingStoreManagerAdapter(storeManager)

	// Initialize and start embedding retry coordinator (multi-store aware)
	embeddingCoordinator := worker.NewEmbeddingRetryCoordinator(
		embeddingAdapter,
		embedder,
		time.Duration(cfg.Worker.EmbeddingRetryInterval),
		cfg.Worker.EmbeddingRetryMaxAttempts,
		cfg.Worker.EmbeddingRetryBatchSize,
	)
	startWorker(ctx, &wg, "embedding-coordinator", embeddingCoordinator.Run)

	// Initialize and start snapshot coordinator (multi-store aware)
	snapshotCoordinator := worker.NewSnapshotCoordinator(
		storeAdapter,
		time.Duration(cfg.Worker.SnapshotInterval),
		uploader,
	)
	startWorker(ctx, &wg, "snapshot-coordinator", snapshotCoordinator.Run)

	// Initialize and start confidence decay coordinator (multi-store aware)
	decayCoordinator := worker.NewDecayCoordinator(
		decayAdapter,
		time.Duration(cfg.Worker.DecayInterval),
		store.DefaultDecayAmount,
	)
	startWorker(ctx, &wg, "decay-coordinator", decayCoordinator.Run)

	// Initialize and start compaction coordinator (multi-store aware)
	compactionAdapter := worker.NewCompactionStoreManagerAdapter(storeManager)
	compactionCoordinator := worker.NewCompactionCoordinator(
		compactionAdapter,
		time.Duration(cfg.Worker.CompactionInterval),
		time.Duration(cfg.Worker.CompactionRetention),
	)
	startWorker(ctx, &wg, "compaction-coordinator", compactionCoordinator.Run)

	// 11. Start HTTP server in goroutine
	go func() {
		slog.Info("server starting", "address", addr)
		// ErrServerClosed is the expected error when Shutdown() is called gracefully.
		// Any other error indicates an actual server failure that should trigger shutdown.
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			cancel() // Trigger shutdown on server failure
		}
	}()

	// 12. Block until signal received
	<-ctx.Done()
	slog.Info("shutdown initiated")

	// 13. Graceful shutdown sequence
	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.Server.ShutdownTimeout))
	defer shutdownCancel()

	// 13a. Stop HTTP server (drains in-flight requests)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}

	// 13b. Wait for workers to complete
	wg.Wait()

	// 13c. Close store
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
// Note: Workers log their own start/stop messages with detailed context.
func startWorker(ctx context.Context, wg *sync.WaitGroup, name string, fn func(ctx context.Context)) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn(ctx)
	}()
}

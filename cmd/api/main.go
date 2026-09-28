// Command api runs the Disaster Response Coordination Platform backend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/example/disaster-response/internal/cache"
	"github.com/example/disaster-response/internal/config"
	"github.com/example/disaster-response/internal/geo"
	"github.com/example/disaster-response/internal/handlers"
	"github.com/example/disaster-response/internal/reports"
	"github.com/example/disaster-response/internal/store"
	"github.com/example/disaster-response/internal/ws"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pg, err := store.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pg.Close()

	rdb, err := cache.NewRedis(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer rdb.Close()

	var extractor geo.Extractor // nil => dictionary-only mode
	if cfg.LLMAPIKey != "" {
		extractor = geo.NewHTTPExtractor(cfg.LLMAPIKey, cfg.LLMBaseURL, cfg.LLMModel)
		log.Info("llm location extraction enabled", "model", cfg.LLMModel)
	} else {
		log.Info("LLM_API_KEY not set; using deterministic gazetteer fallback")
	}

	hub := ws.NewHub(log)
	resolver := geo.NewResolver(extractor, log)
	reportSvc := reports.NewService(reports.MockFeed{}, rdb, reports.CacheTTL, log)
	h := handlers.New(pg, hub, resolver, reportSvc, log)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handlers.NewRouter(h, hub, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub.Shutdown()
	return srv.Shutdown(shutdownCtx)
}

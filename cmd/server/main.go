package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ShamirMaharjan/TaskForge/internal/config"
	"github.com/ShamirMaharjan/TaskForge/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()

	if err != nil {
		log.Printf("Config initalization error: %v", err)
		return 1
	}

	// Signal context listens for termination signals
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(sigCtx, cfg.DatabaseURL)

	if err != nil {
		log.Printf("database initalization error: %v", err)
		return 1
	}
	// Guaranteed to run before run() returns an exit code to main()
	defer pool.Close()

	mux := http.NewServeMux()
	registerHandlers(mux, pool, cfg.Env)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)

	go func() {
		log.Printf("TaskForge api running on port %d [%s]", cfg.Port, cfg.Env)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server died unexpectely: %v", err)
			return 1
		}
		return 0
	case <-sigCtx.Done():
		// Restore standard signal handling so a second Ctrl+C forces an immediate exit
		stop()
		log.Println("Shutdown signal received. Starting 10-second graceful HTTP drain...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		forcedShutdown := false
		if err := server.Shutdown(shutdownCtx); err != nil {
			forcedShutdown = true
			log.Printf("Graceful drain timed out: %v; forcing server closure...", err)
			if closeErr := server.Close(); closeErr != nil {
				log.Printf("Error forcing server close: %v", closeErr)
			}
		}

		if err := <-serverError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server background exit error: %v", err)
		}

		if forcedShutdown {
			log.Println("Server terminated forcefully with active requests dropped.")
			return 1
		}

		log.Println("Server shutdown clean. All connections drained.")
		return 0
	}
}

func registerHandlers(mux *http.ServeMux, pool *pgxpool.Pool, env string) {
	// Liveness: Process health (no dependency checks)
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Ok"))
	})

	// Readiness: Database dependency check
	// Note on LB polling: High-frequency polling (e.g. every 1s) during an outage can produce high log volume.
	// In production, rate-limit health check error logs or route them to a dedicated diagnostic metric.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			log.Printf("Readiness check probe failed: %v", err)
			http.Error(w, "Service Unavailable: Database ping failed", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Ok"))
	})

	// Temporary endpoint for testing graceful drain vs forced timeout
	// Development-only diagnostic handler (Guarded to prevent DoS vectors in staging/production)
	if env == "development" {
		mux.HandleFunc("GET /debug/slow", func(w http.ResponseWriter, r *http.Request) {
			sleepParam := r.URL.Query().Get("sleep")
			duration, err := time.ParseDuration(sleepParam)
			if err != nil {
				duration = 8 * time.Second
			}

			log.Printf("[DEBUG] Processing slow request (%v)...", duration)
			select {
			case <-time.After(duration):
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(fmt.Appendf(nil, "Completed request in %v\n", duration))
			case <-r.Context().Done():
				log.Println("[DEBUG] Request context canceled before completion.")
			}
		})
	}
}

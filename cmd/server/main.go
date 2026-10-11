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
	cfg, err := config.Load()

	if err != nil {
		log.Fatalf("Config initalization error: %v", err)
	}

	// Signal context listens for termination signals
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	defer stop()

	pool, err := db.NewPool(sigCtx, cfg.DatabaseURL)

	if err != nil {
		log.Fatalf("database initalization error: %v", err)
	}

	mux := http.NewServeMux()
	registerHandlers(mux, pool)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)

	go func() {
		log.Printf("TaskForge api running on port %s [%s]", cfg.Port, cfg.Env)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server died unexpectely: %v", err)
		}

	case <-sigCtx.Done():
		log.Println("Shutdown signal recieved. Starting graceful HTTP server drain ....")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Graceful drain timeout, forcing server close: %v", err)
			if closeErr := server.Close(); closeErr != nil {
				log.Printf("Error during force close server: %v", closeErr)
			}
		}

		if err := <-serverError; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Server exit error: %v", err)
		}

		pool.Close()
		log.Println("Database pool closed. Server shutdown clean.")
	}

}

func registerHandlers(mux *http.ServeMux, pool *pgxpool.Pool) {
	// Liveness: Process health (no dependency checks)
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Ok"))
	})

	// Readiness: Database dependency check
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			http.Error(w, "Service Unavailable: Database ping failed", http.StatusServiceUnavailable)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Ok"))
	})

	// Temporary endpoint for testing graceful drain vs forced timeout
	mux.HandleFunc("GET /debug/slow", func(w http.ResponseWriter, r *http.Request) {
		sleepParam := r.URL.Query().Get("sleep")

		duration, err := time.ParseDuration(sleepParam)
		if err != nil {
			duration = 8 * time.Second
		}

		log.Printf("Start slow request processing (%v)...", duration)

		select {
		case <-time.After(duration):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fmt.Appendf(nil, "Completed slow request of %v\n", duration))
			log.Printf("Completed slow request of %v", duration)

		case <-r.Context().Done():
			log.Printf("Slow request context canceled by server shutdown/client")
		}
	})
}

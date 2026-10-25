package platform

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Context() (context.Context, context.CancelFunc) {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func HTTP(ctx context.Context, addr string, mux *http.ServeMux, ready func(context.Context) error) <-chan error {
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		c, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(c); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	done := make(chan error, 1)
	go func() {
		slog.Info("http listening", "address", addr)
		served := make(chan error, 1)
		go func() { served <- server.ListenAndServe() }()
		select {
		case err := <-served:
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			done <- err
			return
		case <-ctx.Done():
		}
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(c)
		if err != nil {
			slog.Error("http shutdown", "error", err)
			_ = server.Close()
		}
		<-served
		done <- err
	}()
	return done
}

// Retry keeps transient dependency failures from dropping a message. Delay is
// bounded and cancellation interrupts both the operation and the backoff.
func Retry(ctx context.Context, operation string, fn func(context.Context) error) error {
	delay := 200 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := fn(attempt)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil {
			return nil
		}
		slog.Warn("operation retry", "operation", operation, "error", err, "delay", delay)
		timer := time.NewTimer(delay + time.Duration(rand.Int64N(int64(delay/4)+1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if delay < 5*time.Second {
			delay *= 2
		}
		if delay > 5*time.Second {
			delay = 5 * time.Second
		}
	}
}

package main

import (
	"context"
	"log/slog"
	"os"

	"diplom/internal/control"
	"diplom/internal/platform"
	"diplom/internal/storage"
)

func run() error {
	ctx, cancel := platform.Context()
	defer cancel()
	db, err := storage.Open(ctx, platform.Env("DATABASE_URL", "postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable"))
	if err != nil {
		return err
	}
	defer db.Pool.Close()
	api := &control.API{DB: db}
	done := platform.HTTP(ctx, platform.Env("HTTP_ADDR", ":8080"), api.Mux(), func(c context.Context) error { return db.Pool.Ping(c) })
	select {
	case <-ctx.Done():
		return <-done
	case err := <-done:
		return err
	}
}
func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

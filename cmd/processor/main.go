package main

import (
	"log/slog"
	"os"

	"diplom/internal/platform"
	"diplom/internal/processing"
)

func main() {
	ctx, cancel := platform.Context()
	defer cancel()
	if err := processing.Run(ctx); err != nil {
		slog.Error("processor stopped", "error", err)
		os.Exit(1)
	}
}

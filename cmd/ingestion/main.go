package main

import (
	"log/slog"
	"os"

	"diplom/internal/ingestion"
	"diplom/internal/platform"
)

func main() {
	ctx, cancel := platform.Context()
	defer cancel()
	if err := ingestion.Run(ctx); err != nil {
		slog.Error("ingestion stopped", "error", err)
		os.Exit(1)
	}
}

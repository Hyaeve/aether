package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"aether/internal/app"
)

func main() {
	configDir := flag.String("config-dir", "config", "Configuration directory")
	dataDir := flag.String("data-dir", "data", "Runtime data directory")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.RunWithDirectories(ctx, *configDir, *dataDir); err != nil {
		log.Fatal(err)
	}
}

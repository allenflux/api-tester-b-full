package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"api-tester/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.New(*configPath)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	defer a.Close()

	if err := a.Run(ctx); err != nil {
		log.Fatalf("run app: %v", err)
	}
}

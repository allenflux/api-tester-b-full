package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"api-tester/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	mode := flag.String("mode", "serve", "serve or export-endpoints")
	flag.Parse()

	a, err := app.New(*configPath)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	defer a.Close()

	switch *mode {
	case "export-endpoints":
		if err := a.ExportEndpointYAML(); err != nil {
			log.Fatalf("export endpoint yaml: %v", err)
		}
		fmt.Fprintln(os.Stdout, "endpoint yaml exported successfully")
		return
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := a.Run(ctx); err != nil {
			log.Fatalf("run app: %v", err)
		}
	default:
		log.Fatalf("unknown mode: %s", *mode)
	}
}

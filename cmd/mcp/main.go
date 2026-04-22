package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"energy-utility/internal/config"
	"energy-utility/internal/mcp"
	"energy-utility/internal/store"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	s3raw, err := store.New(ctx, cfg.S3)
	if err != nil {
		log.Fatalf("init s3: %v", err)
	}
	var s3 store.Store = s3raw
	if cfg.CacheDir != "" {
		s3 = store.NewCached(s3raw, cfg.CacheDir)
		log.Printf("local cache: %s", cfg.CacheDir)
	}

	server := mcp.NewServer()
	if err := mcp.ConfigureServer(ctx, server, s3, cfg); err != nil {
		log.Fatalf("configure mcp server: %v", err)
	}

	transport := mcp.NewStdioTransport(server)

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("shutting down...")
		cancel()
	}()

	log.Println("MCP stdio server running")
	if err := transport.Run(ctx); err != nil {
		log.Fatalf("stdio transport: %v", err)
	}
}

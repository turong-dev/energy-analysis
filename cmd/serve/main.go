package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"energy-utility/internal/config"
	"energy-utility/internal/octopus"
	"energy-utility/internal/store"
)

//go:embed ui
var uiFiles embed.FS

func main() {
	cfgPath := flag.String("config", "config.yaml", "path to config file")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	s3raw, err := store.New(context.Background(), cfg.S3)
	if err != nil {
		log.Fatalf("init s3: %v", err)
	}
	var s3 store.Store = s3raw
	if cfg.CacheDir != "" {
		s3 = store.NewCached(s3raw, cfg.CacheDir)
		log.Printf("local cache: %s", cfg.CacheDir)
	}

	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		log.Fatalf("ui embed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/rates", ratesHandler(s3))
	mux.HandleFunc("GET /api/consumption", consumptionHandler(s3))
	oc := octopus.NewClient(cfg.Octopus.APIKey)
	mux.HandleFunc("GET /api/analysis", analysisHandler(s3, &cfg.Octopus, oc))
	mux.HandleFunc("GET /api/battery/mode-switch", modeSwitchHandler(s3))
	mux.HandleFunc("GET /api/battery/charging-optimisation", chargingOptHandler(s3))
	mux.Handle("/", http.FileServerFS(sub))

	log.Printf("listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

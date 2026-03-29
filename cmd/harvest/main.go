package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"energy-utility/internal/config"
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
	s3, err := store.New(ctx, cfg.S3)
	if err != nil {
		log.Fatalf("init s3: %v", err)
	}

	subcommand := flag.Arg(0)
	switch subcommand {
	case "upload-solax":
		if err := uploadSolax(ctx, s3); err != nil {
			log.Fatalf("upload-solax: %v", err)
		}

	case "fetch-solax":
		fs := flag.NewFlagSet("fetch-solax", flag.ExitOnError)
		sinceStr := fs.String("since", "", "stop at this date YYYY-MM-DD (e.g. installation date)")
		fs.Parse(flag.Args()[1:])

		var since *time.Time
		if *sinceStr != "" {
			t, err := time.Parse("2006-01-02", *sinceStr)
			if err != nil {
				log.Fatalf("invalid --since date: %v", err)
			}
			since = &t
		}
		if err := fetchSolax(ctx, cfg, s3, since); err != nil {
			log.Fatalf("fetch-solax: %v", err)
		}

	case "fetch-octopus":
		if err := fetchOctopus(ctx, cfg, s3); err != nil {
			log.Fatalf("fetch-octopus: %v", err)
		}

	default:
		fmt.Fprintln(os.Stderr, "usage: harvest -config config.yaml <upload-solax|fetch-solax|fetch-octopus>")
		fmt.Fprintln(os.Stderr, "  upload-solax               upload local solax/data/ files to S3")
		fmt.Fprintln(os.Stderr, "  fetch-solax [--since DATE]  fetch from SolaX Cloud into S3")
		fmt.Fprintln(os.Stderr, "  fetch-octopus               fetch Octopus rates and consumption")
		os.Exit(1)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"energy-utility/internal/store"
)

const solaxDataDir = "solax/data"

// uploadSolax reads all local solax/data/YYYY-MM-DD.json files and uploads
// them to S3 under solax/raw/YYYY/MM/DD/daily-detail.json.
// Files already present in S3 are skipped.
func uploadSolax(ctx context.Context, s3 store.Store) error {
	entries, err := os.ReadDir(solaxDataDir)
	if err != nil {
		return fmt.Errorf("read %s: %w", solaxDataDir, err)
	}

	uploaded, skipped := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		date := strings.TrimSuffix(e.Name(), ".json") // YYYY-MM-DD
		parts := strings.Split(date, "-")
		if len(parts) != 3 {
			continue
		}
		key := fmt.Sprintf("solax/raw/%s/%s/%s/daily-detail.json", parts[0], parts[1], parts[2])

		exists, err := s3.Exists(ctx, key)
		if err != nil {
			log.Printf("warn: checking %s: %v", key, err)
		}
		if exists {
			skipped++
			continue
		}

		data, err := os.ReadFile(filepath.Join(solaxDataDir, e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}

		if !json.Valid(data) {
			log.Printf("warn: %s is not valid JSON, skipping", e.Name())
			continue
		}

		if err := s3.PutRaw(ctx, key, "application/json", data); err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		uploaded++
		log.Printf("uploaded %s", key)
	}

	log.Printf("done: %d uploaded, %d already in S3", uploaded, skipped)
	return nil
}

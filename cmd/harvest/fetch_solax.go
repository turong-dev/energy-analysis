package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"energy-utility/internal/config"
	"energy-utility/internal/solax"
	"energy-utility/internal/store"
)

const maxConsecutiveEmpty = 14

func fetchSolax(ctx context.Context, cfg *config.Config, s3 *store.Client, since *time.Time) error {
	client, err := solax.NewClient(cfg.Solax.Email, cfg.Solax.Password, cfg.Solax.CryptoKey, cfg.Solax.CryptoIV)
	if err != nil {
		return fmt.Errorf("create solax client: %w", err)
	}

	today := time.Now().Truncate(24 * time.Hour)
	current := today
	saved, consecutiveEmpty := 0, 0

	for {
		if since != nil && current.Before(*since) {
			log.Printf("reached --since date %s — done", since.Format("2006-01-02"))
			break
		}

		key := solaxKey(current)
		exists, err := s3.Exists(ctx, key)
		if err != nil {
			log.Printf("warn: checking %s: %v", key, err)
		}
		if exists {
			consecutiveEmpty = 0
			current = current.AddDate(0, 0, -1)
			continue
		}

		log.Printf("%s  fetching...", current.Format("2006-01-02"))
		raw, err := client.GetEnergyInfo(cfg.Solax.SiteID, current)
		if err != nil {
			return fmt.Errorf("fetch %s: %w", current.Format("2006-01-02"), err)
		}

		if raw == nil || !client.HasRealData(raw) {
			consecutiveEmpty++
			log.Printf("%s  no data (%d/%d consecutive)", current.Format("2006-01-02"), consecutiveEmpty, maxConsecutiveEmpty)
			if consecutiveEmpty >= maxConsecutiveEmpty {
				log.Printf("max consecutive empty days reached — done")
				break
			}
			current = current.AddDate(0, 0, -1)
			continue
		}

		consecutiveEmpty = 0
		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", current.Format("2006-01-02"), err)
		}
		if err := s3.PutRaw(ctx, key, "application/json", data); err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		saved++
		log.Printf("%s  saved (%d total)", current.Format("2006-01-02"), saved)

		current = current.AddDate(0, 0, -1)
		time.Sleep(500 * time.Millisecond)
	}

	log.Printf("done: %d days saved", saved)
	return nil
}

func solaxKey(day time.Time) string {
	return fmt.Sprintf("solax/raw/%d/%02d/%02d/daily-detail.json", day.Year(), day.Month(), day.Day())
}

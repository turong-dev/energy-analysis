package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"energy-utility/internal/config"
	"energy-utility/internal/store"
	"energy-utility/internal/tariff/octopus"
)

// startDate is the earliest date we have SolaX data for.
var solaxStart = time.Date(2025, 4, 24, 0, 0, 0, 0, time.UTC)

func fetchOctopus(ctx context.Context, cfg *config.Config, s3 store.Store) error {
	client := octopus.NewClient(cfg.Octopus.APIKey)

	region := cfg.Octopus.Region
	if cfg.Octopus.MPANImport != "" {
		r, err := client.GSPRegion(ctx, cfg.Octopus.MPANImport)
		if err != nil {
			log.Printf("warn: could not look up GSP region, falling back to config (%s): %v", region, err)
		} else {
			log.Printf("GSP region: %s", r)
			region = r
		}
	}

	if err := fetchRates(ctx, client, s3, "import", region); err != nil {
		return fmt.Errorf("fetch import rates: %w", err)
	}
	if err := fetchRates(ctx, client, s3, "export", region); err != nil {
		return fmt.Errorf("fetch export rates: %w", err)
	}
	if err := fetchConsumption(ctx, client, s3, "import", cfg.Octopus.MPANImport, cfg.Octopus.MeterSerialImport); err != nil {
		return fmt.Errorf("fetch import consumption: %w", err)
	}
	if err := fetchConsumption(ctx, client, s3, "export", cfg.Octopus.MPANExport, cfg.Octopus.MeterSerialExport); err != nil {
		return fmt.Errorf("fetch export consumption: %w", err)
	}

	return nil
}

func fetchRates(ctx context.Context, client *octopus.Client, s3 store.Store, direction, region string) error {
	log.Printf("fetching agile %s rates...", direction)
	now := time.Now().UTC()
	// Agile rates for the following day are published at 4pm UK time,
	// so extend the fetch window to end of tomorrow when past that time.
	end := agileRatesEnd(now)

	for month := solaxStart; !monthStart(month).After(end); month = month.AddDate(0, 1, 0) {
		from := monthStart(month)
		to := monthEnd(month, end)
		key := fmt.Sprintf("octopus/agile-%s/%d/%02d.json", direction, from.Year(), from.Month())

		// Always re-fetch the latest month as it may be incomplete
		if !isCurrentMonth(from, now) {
			exists, err := s3.Exists(ctx, key)
			if err != nil {
				log.Printf("warn: checking %s: %v", key, err)
			}
			if exists {
				log.Printf("%s: already in S3, skipping", key)
				continue
			}
		}

		// Find the product that was active at the midpoint of this month
		mid := from.AddDate(0, 0, 14)
		if mid.After(now) {
			mid = now
		}
		productCode, err := client.FindAgileProductAt(ctx, direction, mid, log.Printf)
		if err != nil {
			return fmt.Errorf("%d/%02d: find product: %w", from.Year(), from.Month(), err)
		}

		tariffCode, rates, err := client.FetchRates(ctx, productCode, region, from, to)
		if err != nil {
			return fmt.Errorf("%d/%02d: %w", from.Year(), from.Month(), err)
		}
		log.Printf("%s: using product %s (%s)", key, productCode, tariffCode)

		file := octopus.MonthFile[octopus.HalfHourlyRate]{
			Month:   fmt.Sprintf("%d-%02d", from.Year(), from.Month()),
			Updated: now.Format(time.RFC3339),
			Data:    rates,
		}
		if err := s3.PutJSON(ctx, key, file); err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		log.Printf("%s: stored %d rates", key, len(rates))
	}
	return nil
}

func fetchConsumption(ctx context.Context, client *octopus.Client, s3 store.Store, direction, mpan, serial string) error {
	if mpan == "" || serial == "" {
		log.Printf("skipping %s consumption: mpan/serial not configured", direction)
		return nil
	}
	log.Printf("fetching %s consumption (MPAN %s)...", direction, mpan)
	now := time.Now().UTC()

	for month := solaxStart; !monthStart(month).After(now); month = month.AddDate(0, 1, 0) {
		from := monthStart(month)
		to := monthEnd(month, now)
		key := fmt.Sprintf("octopus/consumption/%s/%d/%02d.json", direction, from.Year(), from.Month())

		if !isCurrentMonth(from, now) {
			exists, err := s3.Exists(ctx, key)
			if err != nil {
				log.Printf("warn: checking %s: %v", key, err)
			}
			if exists {
				log.Printf("%s: already in S3, skipping", key)
				continue
			}
		}

		readings, err := client.FetchConsumption(ctx, mpan, serial, from, to)
		if err != nil {
			return fmt.Errorf("%d/%02d: %w", from.Year(), from.Month(), err)
		}

		file := octopus.MonthFile[octopus.HalfHourlyConsumption]{
			Month:   fmt.Sprintf("%d-%02d", from.Year(), from.Month()),
			Updated: now.Format(time.RFC3339),
			Data:    readings,
		}
		if err := s3.PutJSON(ctx, key, file); err != nil {
			return fmt.Errorf("upload %s: %w", key, err)
		}
		log.Printf("%s: stored %d readings", key, len(readings))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Date helpers
// ---------------------------------------------------------------------------

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func monthEnd(t, now time.Time) time.Time {
	end := time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if end.After(now) {
		return now
	}
	return end
}

func isCurrentMonth(t, now time.Time) bool {
	return t.Year() == now.Year() && t.Month() == now.Month()
}

// agileRatesEnd returns the upper bound for fetching Agile rates.
// Rates for the current day are available from midnight; next-day rates
// are published at 4pm UK time, so after that point we extend the window
// to include the full following day. Day boundaries are computed in London
// time so that BST (UTC+1) is handled correctly.
func agileRatesEnd(now time.Time) time.Time {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		return now
	}
	londonNow := now.In(london)
	if londonNow.Hour() >= 16 {
		return time.Date(londonNow.Year(), londonNow.Month(), londonNow.Day()+2, 0, 0, 0, 0, london).UTC()
	}
	return time.Date(londonNow.Year(), londonNow.Month(), londonNow.Day()+1, 0, 0, 0, 0, london).UTC()
}

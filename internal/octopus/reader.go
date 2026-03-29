package octopus

import (
	"context"
	"fmt"
	"time"

	"energy-utility/internal/store"
)

// ReadRates loads all stored half-hourly Agile rates for the given direction
// ("import" or "export") covering every month between from and to (inclusive).
// Months not yet stored in S3 are silently skipped.
func ReadRates(ctx context.Context, s3 store.Store, direction string, from, to time.Time) ([]HalfHourlyRate, error) {
	var all []HalfHourlyRate
	for m := monthStart(from); !m.After(to); m = m.AddDate(0, 1, 0) {
		key := fmt.Sprintf("octopus/agile-%s/%d/%02d.json", direction, m.Year(), m.Month())
		var f MonthFile[HalfHourlyRate]
		if err := s3.GetJSON(ctx, key, &f); err != nil {
			if store.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", key, err)
		}
		all = append(all, f.Data...)
	}
	return all, nil
}

// ReadConsumption loads all stored half-hourly meter readings for the given
// direction ("import" or "export") covering every month between from and to.
// Months not yet stored in S3 are silently skipped.
func ReadConsumption(ctx context.Context, s3 store.Store, direction string, from, to time.Time) ([]HalfHourlyConsumption, error) {
	var all []HalfHourlyConsumption
	for m := monthStart(from); !m.After(to); m = m.AddDate(0, 1, 0) {
		key := fmt.Sprintf("octopus/consumption/%s/%d/%02d.json", direction, m.Year(), m.Month())
		var f MonthFile[HalfHourlyConsumption]
		if err := s3.GetJSON(ctx, key, &f); err != nil {
			if store.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", key, err)
		}
		all = append(all, f.Data...)
	}
	return all, nil
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

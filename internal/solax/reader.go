package solax

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"energy-utility/internal/store"
)

// ParseRaw converts a Raw API response into a DayRecord.
func ParseRaw(raw *Raw, date string) DayRecord {
	toSlice := func(pts []DataPoint) []float64 {
		s := make([]float64, len(pts))
		for i, p := range pts {
			if p.Yais != nil {
				s[i] = *p.Yais
			} else {
				s[i] = math.NaN()
			}
		}
		return s
	}
	return DayRecord{
		Date:             date,
		TotalYield:       raw.Yield.TotalYield,
		FeedIn:           raw.FeedInEnergy,
		GridImport:       raw.Consumed.Grid,
		BatteryCharge:    raw.ChargeYield,
		BatteryDischarge: raw.DisChargeYield,
		TotalLoad:        raw.ConsumeEnergy,
		PVPower:          toSlice(raw.Records.PVPower),
		LoadPower:        toSlice(raw.Records.LoadPower),
		BatteryPower:     toSlice(raw.Records.BatteryPower),
		BatterySoC:       toSlice(raw.Records.BatterySoC),
		FeedInPower:      toSlice(raw.Records.FeedInPower),
	}
}

// ReadDays fetches all available DayRecords from S3, reading concurrently.
// Missing or unreadable days are silently skipped.
func ReadDays(ctx context.Context, s3c *store.Client) ([]DayRecord, error) {
	keys, err := s3c.List(ctx, "solax/raw/")
	if err != nil {
		return nil, fmt.Errorf("list solax days: %w", err)
	}

	type result struct {
		day DayRecord
		ok  bool
	}

	sem := make(chan struct{}, 8)
	out := make(chan result)
	var wg sync.WaitGroup

	for _, key := range keys {
		if !strings.HasSuffix(key, "/daily-detail.json") {
			continue
		}
		// key format: solax/raw/YYYY/MM/DD/daily-detail.json
		parts := strings.Split(key, "/")
		if len(parts) != 6 {
			continue
		}
		date := parts[2] + "-" + parts[3] + "-" + parts[4]

		wg.Add(1)
		go func(k, d string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var raw Raw
			if err := s3c.GetJSON(ctx, k, &raw); err != nil {
				out <- result{}
				return
			}
			out <- result{day: ParseRaw(&raw, d), ok: true}
		}(key, date)
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	var days []DayRecord
	for r := range out {
		if r.ok {
			days = append(days, r.day)
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return days, nil
}

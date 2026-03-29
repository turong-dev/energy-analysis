package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"time"

	"fmt"

	"energy-utility/internal/analysis"
	"energy-utility/internal/config"
	"energy-utility/internal/octopus"
	"energy-utility/internal/store"
)

type dataPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

func ratesHandler(s3 *store.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		direction := r.URL.Query().Get("direction")
		if direction == "" {
			direction = "import"
		}
		if direction != "import" && direction != "export" {
			http.Error(w, "direction must be import or export", http.StatusBadRequest)
			return
		}
		from, to, err := parseDateRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		rates, err := octopus.ReadRates(r.Context(), s3, direction, from, to)
		if err != nil {
			log.Printf("rates: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		points := make([]dataPoint, len(rates))
		for i, rate := range rates {
			points[i] = dataPoint{T: rate.ValidFrom.UTC().Format(time.RFC3339), V: rate.ValueIncVAT}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(points)
	}
}

func consumptionHandler(s3 *store.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		direction := r.URL.Query().Get("direction")
		if direction == "" {
			direction = "import"
		}
		if direction != "import" && direction != "export" {
			http.Error(w, "direction must be import or export", http.StatusBadRequest)
			return
		}
		from, to, err := parseDateRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		readings, err := octopus.ReadConsumption(r.Context(), s3, direction, from, to)
		if err != nil {
			log.Printf("consumption: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		points := make([]dataPoint, len(readings))
		for i, c := range readings {
			points[i] = dataPoint{T: c.IntervalStart.UTC().Format(time.RFC3339), V: c.Consumption}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(points)
	}
}

func parseDateRange(r *http.Request) (from, to time.Time, err error) {
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		// default: last 7 days
		to = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
		from = to.AddDate(0, 0, -7)
		return from, to, nil
	}
	from, err = time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid from date: %s", fromStr)
	}
	to, err = time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid to date: %s", toStr)
	}
	to = to.Add(24 * time.Hour) // make to inclusive
	return from, to, nil
}

func analysisHandler(s3 *store.Client, cfg *config.OctopusConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, err := parseDateRange(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		importRates, err := octopus.ReadRates(r.Context(), s3, "import", from, to)
		if err != nil {
			log.Printf("analysis: import rates: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		exportRates, err := octopus.ReadRates(r.Context(), s3, "export", from, to)
		if err != nil {
			log.Printf("analysis: export rates: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		importConsumption, err := octopus.ReadConsumption(r.Context(), s3, "import", from, to)
		if err != nil {
			log.Printf("analysis: import consumption: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		exportConsumption, err := octopus.ReadConsumption(r.Context(), s3, "export", from, to)
		if err != nil {
			log.Printf("analysis: export consumption: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		days := analysis.Calculate(importRates, exportRates, importConsumption, exportConsumption, cfg)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(days)
	}
}

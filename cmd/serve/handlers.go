package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"energy-utility/internal/analysis"
	"energy-utility/internal/config"
	"energy-utility/internal/device"
	"energy-utility/internal/device/solax"
	"energy-utility/internal/store"
	"energy-utility/internal/tariff"
	"energy-utility/internal/tariff/octopus"
)

type dataPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

func ratesHandler(s3 store.Store) http.HandlerFunc {
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

		rates = filterRates(rates, from, to)
		points := make([]dataPoint, len(rates))
		for i, rate := range rates {
			points[i] = dataPoint{T: rate.ValidFrom.UTC().Format(time.RFC3339), V: rate.ValueIncVAT}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(points)
	}
}

func consumptionHandler(s3 store.Store) http.HandlerFunc {
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

		readings = filterConsumption(readings, from, to)
		points := make([]dataPoint, len(readings))
		for i, c := range readings {
			points[i] = dataPoint{T: c.IntervalStart.UTC().Format(time.RFC3339), V: c.Consumption}
		}
		sort.Slice(points, func(i, j int) bool { return points[i].T < points[j].T })

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(points)
	}
}

func chargingOptHandler(s3 store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days, err := solax.ReadDays(r.Context(), s3)
		if err != nil {
			log.Printf("charging-opt: read days: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		result := analysis.AnalyseCharging(toDeviceDays(days))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func modeSwitchHandler(s3 store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days, err := solax.ReadDays(r.Context(), s3)
		if err != nil {
			log.Printf("mode-switch: read days: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		result := analysis.DetectModeSwitch(toDeviceDays(days))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func filterRates(rates []octopus.HalfHourlyRate, from, to time.Time) []octopus.HalfHourlyRate {
	out := rates[:0:0]
	for _, r := range rates {
		if !r.ValidFrom.Before(from) && r.ValidFrom.Before(to) {
			out = append(out, r)
		}
	}
	return out
}

func filterConsumption(cs []octopus.HalfHourlyConsumption, from, to time.Time) []octopus.HalfHourlyConsumption {
	out := cs[:0:0]
	for _, c := range cs {
		if !c.IntervalStart.Before(from) && c.IntervalStart.Before(to) {
			out = append(out, c)
		}
	}
	return out
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

func toTariffRates(rates []octopus.HalfHourlyRate) []tariff.Rate {
	result := make([]tariff.Rate, len(rates))
	for i, r := range rates {
		result[i] = tariff.Rate{
			ValueIncVAT: r.ValueIncVAT,
			ValidFrom:   r.ValidFrom,
			ValidTo:     r.ValidTo,
		}
	}
	return result
}

func toDeviceDays(days []solax.DayRecord) []device.DayData {
	result := make([]device.DayData, len(days))
	for i, d := range days {
		date, _ := time.Parse("2006-01-02", d.Date)
		result[i] = device.DayData{
			Date:             date,
			Resolution:       5 * time.Minute,
			TotalYield:       d.TotalYield,
			FeedIn:           d.FeedIn,
			GridImport:       d.GridImport,
			BatteryCharge:    d.BatteryCharge,
			BatteryDischarge: d.BatteryDischarge,
			Load:             d.TotalLoad,
			PVPower:          device.TimeSeries{Resolution: 5 * time.Minute, Values: d.PVPower},
			LoadPower:        device.TimeSeries{Resolution: 5 * time.Minute, Values: d.LoadPower},
			BatteryPower:     device.TimeSeries{Resolution: 5 * time.Minute, Values: d.BatteryPower},
			BatterySoC:       device.TimeSeries{Resolution: 5 * time.Minute, Values: d.BatterySoC},
		}
	}
	return result
}

func analysisHandler(s3 store.Store, cfg *config.OctopusConfig, oc *octopus.Client) http.HandlerFunc {
	// Agreements and standing charges rarely change; fetch once per server process.
	var (
		once                sync.Once
		importAgreements    []octopus.TariffAgreement
		exportAgreements    []octopus.TariffAgreement
		agileStandingCharge float64
	)

	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.AccountID != "" {
			once.Do(func() {
				imp, exp, err := oc.FetchAgreements(r.Context(), cfg.AccountID)
				if err != nil {
					log.Printf("analysis: fetch agreements: %v (falling back to Go tariff)", err)
					return
				}
				importAgreements = imp
				exportAgreements = exp
				log.Printf("analysis: loaded %d import + %d export tariff agreements", len(imp), len(exp))

				// Fetch Agile standing charge from the most recent Agile import agreement.
				for i := len(importAgreements) - 1; i >= 0; i-- {
					a := &importAgreements[i]
					if !a.IsAgile() {
						continue
					}
					sc, err := oc.FetchStandingCharge(r.Context(), a.TariffCode)
					if err != nil {
						log.Printf("analysis: fetch Agile standing charge for %s: %v", a.TariffCode, err)
						break
					}
					agileStandingCharge = sc
					log.Printf("analysis: Agile standing charge: %.4fp/day (from %s)", sc, a.TariffCode)
					break
				}
			})
		}

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
		importRates = filterRates(importRates, from, to)
		exportRates = filterRates(exportRates, from, to)
		importConsumption = filterConsumption(importConsumption, from, to)
		exportConsumption = filterConsumption(exportConsumption, from, to)

		result := analysis.Calculate(toTariffRates(importRates), toTariffRates(exportRates), importConsumption, exportConsumption, importAgreements, exportAgreements, cfg, agileStandingCharge)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

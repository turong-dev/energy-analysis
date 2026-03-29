package analysis

import (
	"fmt"
	"sort"
	"time"
	_ "time/tzdata"

	"energy-utility/internal/config"
	"energy-utility/internal/octopus"
)

var london *time.Location

func init() {
	var err error
	london, err = time.LoadLocation("Europe/London")
	if err != nil {
		panic(fmt.Sprintf("load Europe/London timezone: %v", err))
	}
}

// DayCost holds the cost comparison for a single calendar day (London local).
type DayCost struct {
	Date        string  `json:"date"`         // YYYY-MM-DD
	AgileImport float64 `json:"agile_import"` // pence
	GoImport    float64 `json:"go_import"`    // pence
	AgileExport float64 `json:"agile_export"` // pence
	FixedExport float64 `json:"fixed_export"` // pence
	AgileNet    float64 `json:"agile_net"`    // AgileImport - AgileExport
	GoNet       float64 `json:"go_net"`       // GoImport - FixedExport
	Saving      float64 `json:"saving"`       // GoNet - AgileNet (positive = Agile cheaper)
	ImportKWh   float64 `json:"import_kwh"`
	ExportKWh   float64 `json:"export_kwh"`
}

// Calculate computes daily cost comparisons between Agile and Go tariffs.
// Slots with no matching Agile rate contribute zero cost (not an error).
func Calculate(
	importRates, exportRates []octopus.HalfHourlyRate,
	importConsumption, exportConsumption []octopus.HalfHourlyConsumption,
	cfg *config.OctopusConfig,
) []DayCost {
	importRateMap := buildRateMap(importRates)
	exportRateMap := buildRateMap(exportRates)

	days := map[string]*DayCost{}

	get := func(date string) *DayCost {
		if days[date] == nil {
			days[date] = &DayCost{Date: date}
		}
		return days[date]
	}

	for _, c := range importConsumption {
		date := c.IntervalStart.In(london).Format("2006-01-02")
		d := get(date)
		d.ImportKWh += c.Consumption
		d.AgileImport += c.Consumption * importRateMap[c.IntervalStart.UTC().Truncate(time.Minute)]
		d.GoImport += c.Consumption * goRateForSlot(c.IntervalStart, cfg)
	}

	for _, c := range exportConsumption {
		date := c.IntervalStart.In(london).Format("2006-01-02")
		d := get(date)
		d.ExportKWh += c.Consumption
		d.AgileExport += c.Consumption * exportRateMap[c.IntervalStart.UTC().Truncate(time.Minute)]
		d.FixedExport += c.Consumption * cfg.ExportRateAt(c.IntervalStart)
	}

	result := make([]DayCost, 0, len(days))
	for _, d := range days {
		d.AgileNet = d.AgileImport - d.AgileExport
		d.GoNet = d.GoImport - d.FixedExport
		d.Saving = d.GoNet - d.AgileNet
		result = append(result, *d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func buildRateMap(rates []octopus.HalfHourlyRate) map[time.Time]float64 {
	m := make(map[time.Time]float64, len(rates))
	for _, r := range rates {
		m[r.ValidFrom.UTC().Truncate(time.Minute)] = r.ValueIncVAT
	}
	return m
}

func goRateForSlot(t time.Time, cfg *config.OctopusConfig) float64 {
	local := t.In(london)
	rate := cfg.GoRateAt(local)
	if rate == nil {
		return 0
	}
	slotMin := local.Hour()*60 + local.Minute()
	startMin := parseHHMM(rate.OffpeakStart)
	endMin := parseHHMM(rate.OffpeakEnd)

	var inOffpeak bool
	switch {
	case startMin == endMin:
		inOffpeak = true
	case startMin < endMin:
		inOffpeak = slotMin >= startMin && slotMin < endMin
	default: // window crosses midnight
		inOffpeak = slotMin >= startMin || slotMin < endMin
	}

	if inOffpeak {
		return rate.OffpeakRate
	}
	return rate.PeakRate
}

func parseHHMM(s string) int {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h*60 + m
}

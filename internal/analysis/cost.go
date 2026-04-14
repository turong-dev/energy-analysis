package analysis

import (
	"fmt"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"

	"energy-utility/internal/config"
	"energy-utility/internal/tariff"
	"energy-utility/internal/tariff/octopus"
)

var london *time.Location

func init() {
	var err error
	london, err = time.LoadLocation("Europe/London")
	if err != nil {
		panic(fmt.Sprintf("load Europe/London timezone: %v", err))
	}
}

// DayCost holds the cost breakdown for a single calendar day (London local).
//
// Three cost scenarios are computed:
//   - Actual: what was genuinely paid, using the tariff active under the account
//     agreement at each slot (Go or Agile, as applicable). Falls back to Go if
//     no agreement history is available.
//   - Go: hypothetical cost had the Go tariff been active for the entire day.
//   - Agile: hypothetical cost had Agile been active for the entire day.
type DayCost struct {
	Date                 string  `json:"date"`                   // YYYY-MM-DD
	ActualImport         float64 `json:"actual_import"`          // pence
	ActualExport         float64 `json:"actual_export"`          // pence
	ActualNet            float64 `json:"actual_net"`             // ActualImport - ActualExport
	GoImport             float64 `json:"go_import"`              // pence
	GoExport             float64 `json:"go_export"`              // pence (fixed export rate)
	GoNet                float64 `json:"go_net"`                 // GoImport - GoExport
	AgileImport          float64 `json:"agile_import"`           // pence
	AgileExport          float64 `json:"agile_export"`           // pence
	AgileNet             float64 `json:"agile_net"`              // AgileImport - AgileExport
	GoStandingCharge     float64 `json:"go_standing_charge"`     // pence/day (Go tariff)
	AgileStandingCharge  float64 `json:"agile_standing_charge"`  // pence/day (Agile tariff)
	ActualStandingCharge float64 `json:"actual_standing_charge"` // pence/day (tariff active that day)
	ImportKWh            float64 `json:"import_kwh"`
	ExportKWh            float64 `json:"export_kwh"`
}

// TariffPeriod describes a period during which a specific tariff type was active.
type TariffPeriod struct {
	From   string  `json:"from"`         // YYYY-MM-DD
	To     *string `json:"to,omitempty"` // YYYY-MM-DD, nil = ongoing
	Tariff string  `json:"tariff"`       // "Go" | "Agile" | "Unknown"
}

// AnalysisResult bundles the per-day cost breakdown with tariff switch metadata.
type AnalysisResult struct {
	Days          []DayCost      `json:"days"`
	ImportPeriods []TariffPeriod `json:"import_periods"`
}

// AgreementsToPeriods converts raw tariff agreements into labelled display periods.
func AgreementsToPeriods(agreements []octopus.TariffAgreement) []TariffPeriod {
	periods := make([]TariffPeriod, len(agreements))
	for i, a := range agreements {
		tariff := "Unknown"
		code := strings.ToUpper(a.TariffCode)
		switch {
		case strings.Contains(code, "AGILE"):
			tariff = "Agile"
		case strings.Contains(code, "-GO-"):
			tariff = "Go"
		}
		var to *string
		if a.ValidTo != nil {
			s := a.ValidTo.Format("2006-01-02")
			to = &s
		}
		periods[i] = TariffPeriod{From: a.ValidFrom.Format("2006-01-02"), To: to, Tariff: tariff}
	}
	return periods
}

// Calculate computes daily cost breakdowns across three scenarios: actual
// (per-slot tariff from agreement history), hypothetical full-Go, and
// hypothetical full-Agile.
//
// importAgreements and exportAgreements may be nil/empty, in which case
// actual cost falls back to the Go tariff (preserving backward compatibility).
//
// Slots with no matching Agile rate contribute zero Agile cost (not an error).
func Calculate(
	importRates, exportRates []tariff.Rate,
	importConsumption, exportConsumption []octopus.HalfHourlyConsumption,
	importAgreements, exportAgreements []octopus.TariffAgreement,
	cfg *config.OctopusConfig,
	agileStandingCharge float64,
) AnalysisResult {
	importRateMap := buildTariffRateMap(importRates)
	exportRateMap := buildTariffRateMap(exportRates)

	daysMap := map[string]*DayCost{}

	get := func(date string) *DayCost {
		if daysMap[date] == nil {
			daysMap[date] = &DayCost{Date: date}
		}
		return daysMap[date]
	}

	for _, c := range importConsumption {
		date := c.IntervalStart.In(london).Format("2006-01-02")
		d := get(date)
		d.ImportKWh += c.Consumption
		agileRate := importRateMap[c.IntervalStart.UTC().Truncate(time.Minute)]
		goRate := goRateForSlot(c.IntervalStart, cfg)
		d.AgileImport += c.Consumption * agileRate
		d.GoImport += c.Consumption * goRate
		d.ActualImport += c.Consumption * actualImportRate(c.IntervalStart, importAgreements, agileRate, goRate)
	}

	for _, c := range exportConsumption {
		date := c.IntervalStart.In(london).Format("2006-01-02")
		d := get(date)
		d.ExportKWh += c.Consumption
		agileRate := exportRateMap[c.IntervalStart.UTC().Truncate(time.Minute)]
		fixedRate := cfg.ExportRateAt(c.IntervalStart)
		d.AgileExport += c.Consumption * agileRate
		d.GoExport += c.Consumption * fixedRate
		d.ActualExport += c.Consumption * actualExportRate(c.IntervalStart, exportAgreements, agileRate, fixedRate)
	}

	days := make([]DayCost, 0, len(daysMap))
	for _, d := range daysMap {
		d.ActualNet = d.ActualImport - d.ActualExport
		d.GoNet = d.GoImport - d.GoExport
		d.AgileNet = d.AgileImport - d.AgileExport
		d.GoStandingCharge = standingChargeForDay(d.Date, cfg)
		d.AgileStandingCharge = agileStandingCharge
		if isAgileDay(d.Date, importAgreements) {
			d.ActualStandingCharge = agileStandingCharge
		} else {
			d.ActualStandingCharge = d.GoStandingCharge
		}
		days = append(days, *d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	return AnalysisResult{
		Days:          days,
		ImportPeriods: AgreementsToPeriods(importAgreements),
	}
}

// actualImportRate returns the import rate for a slot based on the active
// agreement. Falls back to goRate when no agreement covers the slot.
func actualImportRate(t time.Time, agreements []octopus.TariffAgreement, agileRate, goRate float64) float64 {
	a := octopus.AgreementAt(agreements, t)
	if a != nil && a.IsAgile() {
		return agileRate
	}
	return goRate
}

// actualExportRate returns the export rate for a slot based on the active
// agreement. Falls back to fixedRate when no agreement covers the slot.
func actualExportRate(t time.Time, agreements []octopus.TariffAgreement, agileRate, fixedRate float64) float64 {
	a := octopus.AgreementAt(agreements, t)
	if a != nil && a.IsAgile() {
		return agileRate
	}
	return fixedRate
}

func buildRateMap(rates []octopus.HalfHourlyRate) map[time.Time]float64 {
	m := make(map[time.Time]float64, len(rates))
	for _, r := range rates {
		m[r.ValidFrom.UTC().Truncate(time.Minute)] = r.ValueIncVAT
	}
	return m
}

func buildTariffRateMap(rates []tariff.Rate) map[time.Time]float64 {
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

func isAgileDay(date string, agreements []octopus.TariffAgreement) bool {
	t, err := time.ParseInLocation("2006-01-02", date, london)
	if err != nil {
		return false
	}
	a := octopus.AgreementAt(agreements, t)
	return a != nil && a.IsAgile()
}

func standingChargeForDay(date string, cfg *config.OctopusConfig) float64 {
	t, err := time.ParseInLocation("2006-01-02", date, london)
	if err != nil {
		return 0
	}
	rate := cfg.GoRateAt(t)
	if rate == nil {
		return 0
	}
	return rate.StandingCharge
}

func parseHHMM(s string) int {
	var h, m int
	fmt.Sscanf(s, "%d:%d", &h, &m)
	return h*60 + m
}

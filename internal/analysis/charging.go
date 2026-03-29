package analysis

import (
	"math"
	"sort"
	"time"

	"energy-utility/internal/solax"
)

const depletionThresholdPct = 15.0 // % SoC — below this the battery is considered depleted

// DayCharging holds the charging analysis for a single day.
type DayCharging struct {
	Date           string  `json:"date"`
	TotalYieldKWh  float64 `json:"total_yield_kwh"`
	MinSoC         float64 `json:"min_soc"`          // lowest SoC from 07:00 onwards (%)
	SolarChargeKWh float64 `json:"solar_charge_kwh"` // kWh charged from solar (after 07:00)
	Depleted       bool    `json:"depleted"`          // min SoC hit depletion threshold
	Season         string  `json:"season"`
}

// ChargingSeasonStats summarises the decision boundary for one season.
type ChargingSeasonStats struct {
	BreakevenYieldKWh float64 `json:"breakeven_yield_kwh"`
	DepletedDays      int     `json:"depleted_days"`
	TotalDays         int     `json:"total_days"`
}

// ChargingOptResult is the output of AnalyseCharging.
type ChargingOptResult struct {
	Days              []DayCharging                  `json:"days"`
	BreakevenYieldKWh float64                        `json:"breakeven_yield_kwh"`
	Seasons           map[string]ChargingSeasonStats `json:"seasons"`
}

// AnalyseCharging identifies which days the battery was depleted and derives
// the solar yield threshold below which self-use (solar top-up) mode is needed.
func AnalyseCharging(days []solax.DayRecord) ChargingOptResult {
	var result []DayCharging

	for _, d := range days {
		if len(d.BatterySoC) < 100 {
			continue
		}
		date, err := time.Parse("2006-01-02", d.Date)
		if err != nil {
			continue
		}
		minSoC := computeMinSoC(d.BatterySoC)
		if math.IsNaN(minSoC) {
			continue
		}
		result = append(result, DayCharging{
			Date:           d.Date,
			TotalYieldKWh:  d.TotalYield,
			MinSoC:         minSoC,
			SolarChargeKWh: daytimeChargeKWh(d),
			Depleted:       minSoC <= depletionThresholdPct,
			Season:         seasonFor(date),
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })

	return ChargingOptResult{
		Days:              result,
		BreakevenYieldKWh: chargingBreakeven(result),
		Seasons:           chargingSeasons(result),
	}
}

// computeMinSoC returns the minimum SoC from 07:00 onwards, ignoring the
// pre-dawn period when the battery is still being charged from the grid.
func computeMinSoC(soc []float64) float64 {
	startSlot := 7 * 60 / 5 // 07:00 = slot 84
	if len(soc) <= startSlot {
		return math.NaN()
	}
	min := math.NaN()
	for _, v := range soc[startSlot:] {
		if math.IsNaN(v) {
			continue
		}
		if math.IsNaN(min) || v < min {
			min = v
		}
	}
	return min
}

func seasonFor(t time.Time) string {
	switch t.Month() {
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	case time.September, time.October, time.November:
		return "autumn"
	default:
		return "winter"
	}
}

// chargingBreakeven finds the yield threshold that best separates depleted from
// non-depleted days by minimising misclassifications.
func chargingBreakeven(days []DayCharging) float64 {
	sorted := make([]DayCharging, len(days))
	copy(sorted, days)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TotalYieldKWh < sorted[j].TotalYieldKWh })

	n := len(sorted)
	if n < 2 {
		return 0
	}

	totalDepleted := 0
	for _, d := range sorted {
		if d.Depleted {
			totalDepleted++
		}
	}
	if totalDepleted == 0 || totalDepleted == n {
		return 0
	}

	bestSplit, bestScore := 0, -1
	deplBefore := 0

	for i := 0; i < n-1; i++ {
		if sorted[i].Depleted {
			deplBefore++
		}
		nonDeplAfter := (n - 1 - i) - (totalDepleted - deplBefore)
		if score := deplBefore + nonDeplAfter; score > bestScore {
			bestScore = score
			bestSplit = i
		}
	}

	return (sorted[bestSplit].TotalYieldKWh + sorted[bestSplit+1].TotalYieldKWh) / 2
}

func chargingSeasons(days []DayCharging) map[string]ChargingSeasonStats {
	bySeason := map[string][]DayCharging{}
	for _, d := range days {
		bySeason[d.Season] = append(bySeason[d.Season], d)
	}
	result := map[string]ChargingSeasonStats{}
	for s, sd := range bySeason {
		depleted := 0
		for _, d := range sd {
			if d.Depleted {
				depleted++
			}
		}
		result[s] = ChargingSeasonStats{
			BreakevenYieldKWh: chargingBreakeven(sd),
			DepletedDays:      depleted,
			TotalDays:         len(sd),
		}
	}
	return result
}

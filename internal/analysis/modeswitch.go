package analysis

import (
	"math"
	"sort"

	"energy-utility/internal/solax"
)

// DaySignal holds the mode-switch signal for a single day.
type DaySignal struct {
	Date             string  `json:"date"`
	DaytimeChargeKWh float64 `json:"daytime_charge_kwh"`
	TotalYieldKWh    float64 `json:"total_yield_kwh"`
}

// ModeSwitchResult is the output of DetectModeSwitch.
type ModeSwitchResult struct {
	// SwitchDate is the first day of the new (self-use/charge-priority) regime,
	// or nil if no clear transition was detected.
	SwitchDate *string     `json:"switch_date"`
	Days       []DaySignal `json:"days"`
}

const (
	minYieldKWh   = 1.0 // skip days with less solar than this (too noisy)
	minSwitchJump = 0.5 // minimum jump in per-regime mean to call a switch
)

// DetectModeSwitch infers when the inverter switched from export-priority to
// self-use/charge-priority by detecting when daytime battery charging (after
// 07:00) first appeared consistently in the data.
func DetectModeSwitch(days []solax.DayRecord) ModeSwitchResult {
	var signals []DaySignal
	for _, d := range days {
		if d.TotalYield < minYieldKWh {
			continue
		}
		signals = append(signals, DaySignal{
			Date:             d.Date,
			DaytimeChargeKWh: daytimeChargeKWh(d),
			TotalYieldKWh:    d.TotalYield,
		})
	}
	sort.Slice(signals, func(i, j int) bool { return signals[i].Date < signals[j].Date })

	return ModeSwitchResult{
		SwitchDate: findSwitchDate(signals),
		Days:       signals,
	}
}

// daytimeChargeKWh returns the kWh of battery charging that occurred after 07:00.
// Slot index i corresponds to the 5-minute interval starting at i*5 minutes past midnight.
func daytimeChargeKWh(d solax.DayRecord) float64 {
	total := 0.0
	for i, p := range d.BatteryPower {
		if math.IsNaN(p) || p <= 0 {
			continue
		}
		if i*5 >= 7*60 { // slot starts at or after 07:00
			total += p * (5.0 / 60.0) // kW × hours
		}
	}
	return total
}

// findSwitchDate finds the split point that maximises the jump from pre-split
// mean to post-split mean daytime charging. Returns nil if no clear transition
// is found (jump < minSwitchJump).
func findSwitchDate(signals []DaySignal) *string {
	n := len(signals)
	if n < 20 {
		return nil
	}

	// Precompute prefix sums so we can evaluate all splits in O(n).
	prefix := make([]float64, n+1)
	for i, s := range signals {
		prefix[i+1] = prefix[i] + s.DaytimeChargeKWh
	}

	bestSplit := -1
	bestScore := 0.0

	for split := 7; split < n-7; split++ {
		preMean := prefix[split] / float64(split)
		postMean := (prefix[n] - prefix[split]) / float64(n-split)
		score := postMean - preMean
		if score > bestScore {
			bestScore = score
			bestSplit = split
		}
	}

	if bestSplit < 0 || bestScore < minSwitchJump {
		return nil
	}
	date := signals[bestSplit].Date
	return &date
}

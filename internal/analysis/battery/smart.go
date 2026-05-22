package battery

import (
	"fmt"
	"math"
	"time"

	"energy-utility/internal/analysis"
	"energy-utility/internal/device"
)

type ChargingSlot struct {
	SlotIndex      int     `json:"slot_index"`
	Time           string  `json:"time"`
	EnergyKWh      float64 `json:"energy_kwh"`
	PricePence     float64 `json:"price_pence"`
	SolarKWh       float64 `json:"solar_kwh"`
	EffectivePrice float64 `json:"effective_price_pence"`
}

type DaySimulationResult struct {
	Date                     string         `json:"date"`
	BaselineCostPence        float64        `json:"baseline_cost_pence"`
	OptimizedCostPence       float64        `json:"optimized_cost_pence"`
	PotentialSavingsPence    float64        `json:"potential_savings_pence"`
	CycleCostPence           float64        `json:"cycle_cost_pence"`
	NetSavingsPence          float64        `json:"net_savings_pence"`
	RecommendedCharging      []ChargingSlot `json:"recommended_charging"`
	CyclesUsed               float64        `json:"cycles_used"`
	StartingSoCPercent       float64        `json:"starting_soc_percent"`
	EndingSoCPercent         float64        `json:"ending_soc_percent"`
	TotalImportKWh           float64        `json:"total_import_kwh"`
	TotalExportKWh           float64        `json:"total_export_kwh"`
	LookaheadPricesAvailable bool           `json:"lookahead_prices_available"`
}

type OptimizationSummary struct {
	TotalDays                  int     `json:"total_days"`
	TotalBaselinePence         float64 `json:"total_baseline_pence"`
	TotalOptimizedPence        float64 `json:"total_optimized_pence"`
	TotalPotentialSavingsPence float64 `json:"total_potential_savings_pence"`
	TotalCycleCostPence        float64 `json:"total_cycle_cost_pence"`
	TotalNetSavingsPence       float64 `json:"total_net_savings_pence"`
	AvgSavingsPerDayPence      float64 `json:"avg_savings_per_day_pence"`
	TotalCyclesUsed            float64 `json:"total_cycles_used"`
	DaysWithSavings            int     `json:"days_with_savings"`
	DaysWithoutSavings         int     `json:"days_without_savings"`
}

type ChargingOptimizationResponse struct {
	Days    []DaySimulationResult `json:"days"`
	Summary OptimizationSummary   `json:"summary"`
}

type DaySimulationInput struct {
	Date        string
	ImportRates []float64
	ExportRates []float64
	SolarPower  []float64
	LoadPower   []float64
	InitialSoC  float64
	Config      device.BatteryConfig
}

const halfHour = 30 * time.Minute

func SimulateDay(input DaySimulationInput) DaySimulationResult {
	if len(input.ImportRates) != 48 || len(input.ExportRates) != 48 {
		return DaySimulationResult{
			Date: input.Date,
		}
	}

	baseline := runSimulation(input, false)
	optimized := runSimulation(input, true)

	potentialSavings := baseline.CostPence - optimized.CostPence
	// Only charge cycle cost for the additional cycling caused by grid charging
	marginalCycles := optimized.CyclesUsed - baseline.CyclesUsed
	if marginalCycles < 0 {
		marginalCycles = 0
	}
	cycleCost := marginalCycles * input.Config.CycleCostPence
	netSavings := potentialSavings - cycleCost

	return DaySimulationResult{
		Date:                     input.Date,
		BaselineCostPence:        baseline.CostPence,
		OptimizedCostPence:       optimized.CostPence,
		PotentialSavingsPence:    potentialSavings,
		CycleCostPence:           cycleCost,
		NetSavingsPence:          netSavings,
		RecommendedCharging:      optimized.ChargingSlots,
		CyclesUsed:               optimized.CyclesUsed,
		StartingSoCPercent:       input.InitialSoC,
		EndingSoCPercent:         optimized.EndingSoC,
		TotalImportKWh:           optimized.TotalImport,
		TotalExportKWh:           optimized.TotalExport,
		LookaheadPricesAvailable: true,
	}
}

type simulationResult struct {
	CostPence     float64
	ChargingSlots []ChargingSlot
	CyclesUsed    float64
	EndingSoC     float64
	TotalImport   float64
	TotalExport   float64
}

func runSimulation(input DaySimulationInput, optimize bool) simulationResult {
	bs := analysis.NewBatterySimulator(input.Config, input.InitialSoC)

	result := simulationResult{
		ChargingSlots: make([]ChargingSlot, 0),
	}

	for i := 0; i < 48; i++ {
		solarKWh := input.SolarPower[i]
		loadKWh := input.LoadPower[i]
		importPrice := input.ImportRates[i]
		exportPrice := input.ExportRates[i]

		if optimize {
			chargeAmount := calculateOptimalCharge(bs, input.ImportRates, i, input.Config)
			if chargeAmount > 0 {
				actualCharged := bs.Charge(chargeAmount)
				if actualCharged > 0 {
					result.CostPence += actualCharged * importPrice
					result.TotalImport += actualCharged
					solarFraction := solarKWh / (solarKWh + actualCharged)
					effectivePrice := (importPrice * (1 - solarFraction))
					slot := ChargingSlot{
						SlotIndex:      i,
						Time:           slotTime(i),
						EnergyKWh:      actualCharged,
						PricePence:     importPrice,
						SolarKWh:       solarKWh,
						EffectivePrice: effectivePrice,
					}
					result.ChargingSlots = append(result.ChargingSlots, slot)
				}
			}
		}

		if solarKWh >= loadKWh {
			excessSolar := solarKWh - loadKWh
			usedByBattery := bs.Charge(excessSolar)
			if usedByBattery < excessSolar {
				exported := excessSolar - usedByBattery
				result.CostPence -= exported * exportPrice
				result.TotalExport += exported
			}
		} else {
			deficit := loadKWh - solarKWh
			fromBattery := bs.Discharge(deficit)
			fromGrid := deficit - fromBattery
			if fromGrid > 0 {
				result.CostPence += fromGrid * importPrice
				result.TotalImport += fromGrid
			}
		}
	}

	result.CyclesUsed = bs.GetCyclesUsed()
	result.EndingSoC = bs.GetSoCPercent()

	return result
}

func calculateOptimalCharge(bs *analysis.BatterySimulator, prices []float64, slotIndex int, cfg device.BatteryConfig) float64 {
	currentSoC := bs.GetSoCPercent()
	if currentSoC >= cfg.MaxSoCPercent {
		return 0
	}

	importPrice := prices[slotIndex]
	if importPrice <= 0 {
		return 0
	}

	// Look ahead 16 hours (or to end of day)
	endSlot := slotIndex + 32
	if endSlot > 48 {
		endSlot = 48
	}
	if endSlot <= slotIndex {
		return 0
	}

	// Find the minimum price in the lookahead window
	minPrice := prices[slotIndex]
	for i := slotIndex; i < endSlot; i++ {
		if prices[i] < minPrice {
			minPrice = prices[i]
		}
	}

	// Only charge at or near the cheapest slots in the window
	// (allow a small margin so we don't miss borderline profitable slots)
	if importPrice > minPrice+2.0 {
		return 0
	}

	// Check that future savings justify the charge
	avgFuturePrice := calculateAverageFuturePrice(prices, slotIndex)
	cycleCostPerKWh := cfg.CycleCostPence / cfg.CapacityKWh
	savingsPerKWh := avgFuturePrice*cfg.RoundTripEfficiency - importPrice - cycleCostPerKWh
	if savingsPerKWh <= 0 {
		return 0
	}

	availableCapacity := bs.GetAvailableCapacityKWh()
	maxChargeRate := cfg.MaxChargeKW * 0.5
	return min(availableCapacity, maxChargeRate)
}

func calculateAverageFuturePrice(prices []float64, fromSlot int) float64 {
	if fromSlot >= len(prices)-1 {
		return prices[fromSlot]
	}

	// Collect future prices and sort to find the upper half average
	// (we discharge during expensive slots, so average of all future prices
	// understates the value of stored energy)
	future := make([]float64, 0, len(prices)-fromSlot)
	for i := fromSlot; i < len(prices); i++ {
		future = append(future, prices[i])
	}
	if len(future) == 0 {
		return prices[fromSlot]
	}

	// Sort ascending
	for i := 0; i < len(future); i++ {
		for j := i + 1; j < len(future); j++ {
			if future[j] < future[i] {
				future[i], future[j] = future[j], future[i]
			}
		}
	}

	// Average the upper half
	mid := len(future) / 2
	if mid == 0 {
		mid = 1
	}
	sum := 0.0
	for i := mid; i < len(future); i++ {
		sum += future[i]
	}
	return sum / float64(len(future)-mid)
}

func slotTime(index int) string {
	hours := index / 2
	minutes := (index % 2) * 30
	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

func AggregateToHalfHour(solar5min, load5min []float64) (solarHH, loadHH []float64) {
	if len(solar5min) == 0 || len(load5min) == 0 {
		return []float64{}, []float64{}
	}

	solarHH = make([]float64, 48)
	loadHH = make([]float64, 48)

	for i := 0; i < 48; i++ {
		start := i * 6
		end := start + 6
		if end > len(solar5min) {
			end = len(solar5min)
		}
		if start >= len(solar5min) {
			break
		}

		for j := start; j < end; j++ {
			if j < len(solar5min) && !math.IsNaN(solar5min[j]) {
				solarHH[i] += solar5min[j]
			}
			if j < len(load5min) && !math.IsNaN(load5min[j]) {
				loadHH[i] += load5min[j]
			}
		}
		// Convert from W at 5-min resolution to kWh per half-hour
		solarHH[i] /= 12000.0
		loadHH[i] /= 12000.0
	}

	return solarHH, loadHH
}

func SummarizeResults(days []DaySimulationResult) OptimizationSummary {
	if len(days) == 0 {
		return OptimizationSummary{}
	}

	var summary OptimizationSummary
	summary.TotalDays = len(days)

	for _, d := range days {
		summary.TotalBaselinePence += d.BaselineCostPence
		summary.TotalOptimizedPence += d.OptimizedCostPence
		summary.TotalPotentialSavingsPence += d.PotentialSavingsPence
		summary.TotalCycleCostPence += d.CycleCostPence
		summary.TotalNetSavingsPence += d.NetSavingsPence
		summary.TotalCyclesUsed += d.CyclesUsed
		if d.NetSavingsPence > 0 {
			summary.DaysWithSavings++
		} else {
			summary.DaysWithoutSavings++
		}
	}

	if summary.TotalDays > 0 {
		summary.AvgSavingsPerDayPence = summary.TotalNetSavingsPence / float64(summary.TotalDays)
	}

	return summary
}

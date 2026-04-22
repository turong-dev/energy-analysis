package battery

import (
	"fmt"
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
	cycleCost := optimized.CyclesUsed * input.Config.CycleCostPence
	netSavings := potentialSavings - cycleCost

	if netSavings < 0 {
		netSavings = 0
		potentialSavings = 0
	}

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
	threshold := calculateDynamicThreshold(prices, slotIndex, cfg)

	if importPrice > threshold {
		return 0
	}

	availableCapacity := bs.GetAvailableCapacityKWh()
	maxChargeRate := cfg.MaxChargeKW * 0.5
	chargeAmount := min(availableCapacity, maxChargeRate)

	savingsPerKWh := calculateAverageFuturePrice(prices, slotIndex) - importPrice
	cycleCostPerKWh := cfg.CycleCostPence / cfg.CapacityKWh

	if savingsPerKWh <= cycleCostPerKWh {
		return 0
	}

	return chargeAmount
}

func calculateDynamicThreshold(prices []float64, slotIndex int, cfg device.BatteryConfig) float64 {
	hoursAhead := 0
	if slotIndex < 32 {
		hoursAhead = 16
	} else {
		hoursAhead = 48 - slotIndex
	}

	endSlot := slotIndex + hoursAhead/2
	if endSlot > 48 {
		endSlot = 48
	}

	if endSlot <= slotIndex {
		return 100
	}

	minPrice := prices[slotIndex]
	for i := slotIndex; i < endSlot; i++ {
		if prices[i] < minPrice {
			minPrice = prices[i]
		}
	}

	threshold := minPrice + 1.0

	efficiencyPenalty := (1.0 - cfg.RoundTripEfficiency) * calculateAverageFuturePrice(prices, slotIndex)
	threshold -= efficiencyPenalty

	return threshold
}

func calculateAverageFuturePrice(prices []float64, fromSlot int) float64 {
	if fromSlot >= len(prices)-1 {
		return prices[fromSlot]
	}

	sum := 0.0
	count := 0
	for i := fromSlot; i < len(prices); i++ {
		sum += prices[i]
		count++
	}
	if count == 0 {
		return prices[fromSlot]
	}
	return sum / float64(count)
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
			if j < len(solar5min) {
				solarHH[i] += solar5min[j]
			}
			if j < len(load5min) {
				loadHH[i] += load5min[j]
			}
		}
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

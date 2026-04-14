# Smart Charging Optimization - Implementation Plan

## Overview

Historical simulation to determine optimal battery charging times on Octopus Agile tariff, with respect for battery lifetime and realistic constraints.

**Goal**: Answer "Given what we knew at 4pm the day before (Agile prices), when should we have charged the battery from grid to minimize costs?"

---

## Design Decisions Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| **Forecast type** | Perfect hindsight (theoretical max) | First iteration to establish upper bound |
| **Price threshold** | Dynamic | Calculate based on upcoming expensive periods |
| **Solar blending** | Actual contribution per slot | Accurate effective pricing |
| **Battery modeling** | Full SoC tracking with rate limits | Realistic for automation planning |
| **Lookahead** | 4pm previous day | When Agile prices publish |
| **Rate switch timing** | From 12am same day | Rate changes apply from midnight - no mid-day transitions |
| **Optimization scope** | Single day | As requested |
| **Cycle cost** | Configurable (default 80p/full cycle) | Battery degradation economics |
| **Export optimization** | Secondary (excess only) | No artificial cycling for export arbitrage |

---

## Battery Parameters (Configurable)

```yaml
battery:
  capacity_kwh: 12.0              # Total battery capacity
  max_charge_kw: 7.5              # Max grid charging rate
  max_discharge_kw: 7.5           # Max discharge rate  
  min_soc_percent: 10             # Minimum state of charge
  max_soc_percent: 100            # Maximum state of charge
  round_trip_efficiency: 0.90     # Charge→discharge efficiency loss
  cycle_cost_pence: 80.0          # Cost of one 0-100-0% cycle

optimization:
  lookahead_hours: 24             # How far ahead prices are known
  price_publish_hour: 16          # 4pm when prices publish
```

---

## Core Algorithm

### Phase 1: Baseline Simulation (No Grid Charging)

```
For each 30-minute slot in the day:
  1. Apply solar generation
  2. Meet load from: solar → battery → grid
  3. If solar > load + battery capacity: export excess
  4. Track: grid_import_kwh, grid_export_kwh, battery_soc
  
Result: baseline_cost = Σ(grid_import × import_price) - Σ(grid_export × export_price)
```

### Phase 2: Optimized Simulation (Strategic Grid Charging)

```
For each 30-minute slot:
  
  // Step 1: Identify charging opportunity
  IF slot_price < dynamic_threshold AND battery_soc < max_soc:
    
    // Step 2: Calculate how much to charge
    available_capacity = (max_soc - battery_soc) / 100 × capacity_kwh
    charge_rate_limit = max_charge_kw × 0.5 hours
    
    // Step 3: Calculate how much we'll need before next cheap window
    upcoming_load = calculate_load_until_next_cheap_slot()
    upcoming_solar = calculate_solar_until_next_cheap_slot()
    energy_needed = max(0, upcoming_load - upcoming_solar - (battery_soc/100 × capacity_kwh))
    
    // Step 4: Charge decision
    charge_amount = min(available_capacity, charge_rate_limit, energy_needed)
    
    // Step 5: Calculate effective cost
    solar_fraction = solar_kwh / (solar_kwh + charge_amount)
    effective_price = (slot_price × (1 - solar_fraction)) + (0 × solar_fraction)
    
    // Step 6: Verify savings exceed cycle cost
    savings_per_kwh = average_future_import_price - effective_price
    if savings_per_kwh × charge_amount > cycle_cost_per_kwh × charge_amount:
      charge_from_grid(charge_amount)
  
  // Step 7: Meet load (same as baseline)
  meet_load_from_solar_then_battery_then_grid()
  
  // Step 8: Track cycles
  cycles_used += energy_cycled_through_battery / capacity_kwh

Result: optimized_cost = Σ(grid_import × import_price) - Σ(grid_export × export_price)
```

### Phase 3: Dynamic Threshold Calculation

```
For each slot, the threshold is:
  threshold = min_price_in_next_N_hours + margin
  
Where:
  - N = hours until we expect next significant solar generation
  - margin = small buffer to avoid marginal charges (configurable, default 1p)
  
Alternative formulation:
  threshold = weighted_average_of_upcoming_expensive_slots - efficiency_penalty
```

---

## Data Structures

### Input

```go
// DaySimulationInput contains all data needed to simulate one day
type DaySimulationInput struct {
    Date              string
    ImportRates       []float64  // 48 slots, p/kWh (known at 4pm previous day)
    ExportRates       []float64  // 48 slots, p/kWh (known at 4pm previous day)
    SolarPower        []float64  // 48 slots, kWh per half-hour (aggregated from 5-min)
    LoadPower         []float64  // 48 slots, kWh per half-hour (aggregated from 5-min)
    InitialSoC        float64    // Starting battery %
    Config            BatteryConfig
}

type BatteryConfig struct {
    CapacityKWh          float64
    MaxChargeKW          float64
    MaxDischargeKW       float64
    MinSoCPercent        float64
    MaxSoCPercent        float64
    RoundTripEfficiency  float64
    CycleCostPence       float64
}
```

### Output

```go
// DaySimulationResult contains optimization results for one day
type DaySimulationResult struct {
    Date                     string                  `json:"date"`
    BaselineCostPence        float64                 `json:"baseline_cost_pence"`
    OptimizedCostPence       float64                 `json:"optimized_cost_pence"`
    PotentialSavingsPence    float64                 `json:"potential_savings_pence"`
    CycleCostPence           float64                 `json:"cycle_cost_pence"`
    NetSavingsPence          float64                 `json:"net_savings_pence"`
    RecommendedCharging      []ChargingSlot          `json:"recommended_charging"`
    CyclesUsed               float64                 `json:"cycles_used"`
    StartingSoC              float64                 `json:"starting_soc_percent"`
    EndingSoC                float64                 `json:"ending_soc_percent"`
    TotalImportKWh           float64                 `json:"total_import_kwh"`
    TotalExportKWh           float64                 `json:"total_export_kwh"`
    LookaheadPricesAvailable bool                    `json:"lookahead_prices_available"`
}

type ChargingSlot struct {
    SlotIndex      int     `json:"slot_index"`        // 0-47
    Time           string  `json:"time"`              // "HH:MM"
    EnergyKWh      float64 `json:"energy_kwh"`
    PricePence     float64 `json:"price_pence"`
    SolarKWh       float64 `json:"solar_kwh"`         // Solar generation this slot
    EffectivePrice float64 `json:"effective_price_pence"` // Blended with solar
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
```

### API Response

```go
// GET /api/charging/optimization?from=2025-04-01&to=2025-04-30
type ChargingOptimizationResponse struct {
    Days    []DaySimulationResult `json:"days"`
    Summary OptimizationSummary   `json:"summary"`
}
```

---

## File Structure

### New Files

1. **`internal/analysis/smart_charging.go`** (~350 lines)
   - `SimulateDay(input DaySimulationInput) DaySimulationResult`
   - `SimulateRange(ctx context.Context, s3 store.Store, from, to time.Time, cfg BatteryConfig) ([]DaySimulationResult, error)`
   - `aggregateToHalfHour(records []solax.DayRecord) ([]float64, []float64)`
   - `calculateDynamicThreshold(prices []float64, slotIndex int) float64`
   - `calculateEffectivePrice(importPrice, solarKWh, gridKWh float64) float64`
   - `shouldCharge(prices []float64, slotIndex int, batterySoC float64, upcomingLoad, upcomingSolar float64) (bool, float64)`

2. **`internal/analysis/charging_simulator.go`** (~200 lines) - Battery state simulation
   - `type BatterySimulator struct`
   - `func (bs *BatterySimulator) Charge(amountKWh float64, source string) float64` // Returns actual amount charged
   - `func (bs *BatterySimulator) Discharge(amountKWh float64) float64` // Returns actual amount discharged
   - `func (bs *BatterySimulator) GetSoC() float64`
   - `func (bs *BatterySimulator) GetCyclesUsed() float64`

### Modified Files

3. **`internal/config/config.go`** (~30 lines added)
   - Add `BatteryConfig` struct
   - Add battery section to Config struct
   - Add default values

4. **`cmd/serve/handlers.go`** (~60 lines added)
   - `smartChargingHandler(s3 store.Store, cfg *config.Config) http.HandlerFunc`
   - Parse date range from query params
   - Load battery config
   - Call `analysis.SimulateRange()`
   - Return JSON response

5. **`config.example.yaml`** (~15 lines added)
   - Add battery configuration example

6. **`config.yaml`** (~15 lines added)
   - Add battery configuration (user to fill in actual values)

---

## Example Output

### Single Day Result

```json
{
  "date": "2025-04-12",
  "baseline_cost_pence": 1450,
  "optimized_cost_pence": 1280,
  "potential_savings_pence": 170,
  "cycle_cost_pence": 24,
  "net_savings_pence": 146,
  "recommended_charging": [
    {
      "slot_index": 2,
      "time": "01:00",
      "energy_kwh": 3.0,
      "price_pence": 2.5,
      "solar_kwh": 0.0,
      "effective_price_pence": 2.5
    },
    {
      "slot_index": 3,
      "time": "01:30",
      "energy_kwh": 3.0,
      "price_pence": 2.3,
      "solar_kwh": 0.0,
      "effective_price_pence": 2.3
    },
    {
      "slot_index": 46,
      "time": "23:00",
      "energy_kwh": 2.0,
      "price_pence": -1.0,
      "solar_kwh": 0.0,
      "effective_price_pence": -1.0
    }
  ],
  "cycles_used": 0.3,
  "starting_soc_percent": 35,
  "ending_soc_percent": 45,
  "total_import_kwh": 18.5,
  "total_export_kwh": 4.2,
  "lookahead_prices_available": true
}
```

### Summary Result

```json
{
  "total_days": 30,
  "total_baseline_pence": 43500,
  "total_optimized_pence": 38400,
  "total_potential_savings_pence": 5100,
  "total_cycle_cost_pence": 720,
  "total_net_savings_pence": 4380,
  "avg_savings_per_day_pence": 146,
  "total_cycles_used": 9.0,
  "days_with_savings": 22,
  "days_without_savings": 8
}
```

---

## Testing Strategy

### Unit Tests (Future - not in initial implementation)

1. **Plunge pricing day**: -4p import at night
   - Expected: Max charging during negative price slots
   
2. **High peak day**: 30p peak, 2p overnight
   - Expected: Charge overnight, discharge during peak
   
3. **High solar day**: 20kWh+ solar generation
   - Expected: Minimal/no grid charging, prioritize solar
   
4. **Expensive all day**: No cheap slots
   - Expected: No grid charging recommended
   
5. **Battery constrained**: Already at 100% SoC at cheap slot
   - Expected: Skip charging, no room available

### Integration Tests

1. **End-to-end simulation**: Pick 5 known days, verify calculations manually
2. **Data alignment**: Verify 5-min → 30-min aggregation matches expectations
3. **Cycle calculation**: Verify cycles_used accumulates correctly

---

## Future Extensions (Not in initial implementation)

1. **Pattern learning**: Cluster days by solar + weekday, extract optimal thresholds per pattern
2. **Forecast integration**: Use actual weather/solar forecasts instead of perfect hindsight
3. **Multi-day optimization**: Look ahead multiple days for strategic pre-charging
4. **Export arbitrage**: Model intentional discharge-to-grid during high export prices
5. **UI visualization**: Timeline view showing prices, solar, recommended charge slots

---

## Open Questions / Decisions Needed

1. **Starting SoC**: How should we determine the starting battery SoC for each day?
   - Option A: Use actual SoC from 00:00 in the SolaX data
   - Option B: Assume 100% (fully charged from overnight Go charging)
   - Option C: Track SoC from previous day's simulation
   - **Recommendation**: Option A (most realistic)

2. **Ending SoC handling**: Should we care about ending SoC for cost calculations?
   - Option A: No, each day independent
   - Option B: Yes, value remaining energy at average import price
   - **Recommendation**: Option A for simplicity, but track ending SoC for information

3. **Solar aggregation**: How to aggregate 5-min solar to 30-min?
   - Option A: Simple average × 0.5 hours
   - Option B: Sum all 5-min values
   - **Recommendation**: Option B (sum of 6 × 5-min values = 30-min total kWh)

4. **Rate limiting**: Should we model that battery can't charge and discharge simultaneously?
   - **Answer**: Yes, realistic constraint

5. **Efficiency application**: When should round-trip efficiency be applied?
    - Option A: On discharge (energy out = stored × 0.9)
    - Option B: On charge (energy stored = input × 0.9)
    - **Recommendation**: Option A (simpler mental model)

6. **Rate switch handling**: How to handle days where a tariff switch occurs?
    - **Answer**: Rate switches take effect from 12am the same day. This means each calendar day uses a single consistent rate set - no mid-day tariff transitions needed. When a switch occurs on day D, the new rates apply for the entire day D.

---

## Estimated Effort

- Core simulation logic: ~3-4 hours
- Battery simulator: ~1-2 hours
- Config integration: ~30 minutes
- Handler/endpoint: ~30 minutes
- Testing/debugging: ~2-3 hours
- **Total**: ~8-10 hours of focused work

---

## Files Changed Summary

| File | Change Type | Lines | Purpose |
|------|-------------|-------|---------|
| `internal/analysis/smart_charging.go` | New | ~350 | Core optimization algorithm |
| `internal/analysis/charging_simulator.go` | New | ~200 | Battery state simulation |
| `internal/config/config.go` | Modify | +30 | Battery config struct |
| `cmd/serve/handlers.go` | Modify | +60 | API endpoint |
| `config.example.yaml` | Modify | +15 | Example config |
| `config.yaml` | Modify | +15 | User config (manual) |
| **Total** | | **~670** | |

---

*Document Version: 1.0*
*Last Updated: 2026-04-12*
*Status: Ready for review*

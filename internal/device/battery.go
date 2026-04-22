package device

type BatteryConfig struct {
	CapacityKWh         float64 `yaml:"capacity_kwh"`
	MaxChargeKW         float64 `yaml:"max_charge_kw"`
	MaxDischargeKW      float64 `yaml:"max_discharge_kw"`
	MinSoCPercent       float64 `yaml:"min_soc_percent"`
	MaxSoCPercent       float64 `yaml:"max_soc_percent"`
	RoundTripEfficiency float64 `yaml:"round_trip_efficiency"`
	CycleCostPence      float64 `yaml:"cycle_cost_pence"`
}

func DefaultBatteryConfig() BatteryConfig {
	return BatteryConfig{
		CapacityKWh:         12.0,
		MaxChargeKW:         7.5,
		MaxDischargeKW:      7.5,
		MinSoCPercent:       10.0,
		MaxSoCPercent:       100.0,
		RoundTripEfficiency: 0.90,
		CycleCostPence:      80.0,
	}
}

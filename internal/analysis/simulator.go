package analysis

import (
	"energy-utility/internal/device"
)

type BatterySimulator struct {
	cfg                device.BatteryConfig
	socKWh             float64
	cyclesUsed         float64
	totalChargedKWh    float64
	totalDischargedKWh float64
}

func NewBatterySimulator(cfg device.BatteryConfig, initialSoCPercent float64) *BatterySimulator {
	socKWh := (initialSoCPercent / 100) * cfg.CapacityKWh
	return &BatterySimulator{
		cfg:    cfg,
		socKWh: socKWh,
	}
}

func (bs *BatterySimulator) Charge(amountKWh float64) float64 {
	maxCharge := bs.cfg.MaxChargeKW * 0.5
	if amountKWh > maxCharge {
		amountKWh = maxCharge
	}

	maxSocKWh := (bs.cfg.MaxSoCPercent / 100) * bs.cfg.CapacityKWh
	availableSpace := maxSocKWh - bs.socKWh

	actual := min(amountKWh, availableSpace)
	if actual < 0 {
		actual = 0
	}

	bs.socKWh += actual
	bs.totalChargedKWh += actual
	bs.cyclesUsed += actual / bs.cfg.CapacityKWh

	return actual
}

func (bs *BatterySimulator) Discharge(amountKWh float64) float64 {
	maxDischarge := bs.cfg.MaxDischargeKW * 0.5
	if amountKWh > maxDischarge {
		amountKWh = maxDischarge
	}

	minSocKWh := (bs.cfg.MinSoCPercent / 100) * bs.cfg.CapacityKWh
	availableEnergy := bs.socKWh - minSocKWh

	actual := min(amountKWh, availableEnergy)
	if actual < 0 {
		actual = 0
	}

	energyOut := actual * bs.cfg.RoundTripEfficiency
	bs.socKWh -= actual
	bs.totalDischargedKWh += energyOut

	return energyOut
}

func (bs *BatterySimulator) GetSoCPercent() float64 {
	return (bs.socKWh / bs.cfg.CapacityKWh) * 100
}

func (bs *BatterySimulator) GetSoCKWh() float64 {
	return bs.socKWh
}

func (bs *BatterySimulator) GetCyclesUsed() float64 {
	return bs.cyclesUsed
}

func (bs *BatterySimulator) GetTotalChargedKWh() float64 {
	return bs.totalChargedKWh
}

func (bs *BatterySimulator) GetTotalDischargedKWh() float64 {
	return bs.totalDischargedKWh
}

func (bs *BatterySimulator) GetAvailableCapacityKWh() float64 {
	maxSocKWh := (bs.cfg.MaxSoCPercent / 100) * bs.cfg.CapacityKWh
	return maxSocKWh - bs.socKWh
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

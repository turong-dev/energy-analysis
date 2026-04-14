package device

import (
	"context"
	"time"
)

type TimeSeries struct {
	Resolution time.Duration
	Values     []float64
}

type DayData struct {
	Date             time.Time
	Resolution       time.Duration
	TotalYield       float64
	FeedIn           float64
	GridImport       float64
	BatteryCharge    float64
	BatteryDischarge float64
	Load             float64

	PVPower      TimeSeries
	GridPower    TimeSeries
	LoadPower    TimeSeries
	BatteryPower TimeSeries
	BatterySoC   TimeSeries
}

func (d *DayData) HasRealData() bool {
	return d.TotalYield > 0 || d.Load > 0
}

type EnergyDevice interface {
	Manufacturer() string
	Model() string
	DataResolution() time.Duration
	FetchDay(ctx context.Context, siteID string, day time.Time) (*DayData, error)
}

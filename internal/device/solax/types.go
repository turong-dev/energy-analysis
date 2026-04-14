package solax

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// DayRecord is the normalised representation of one day's data,
// derived from the raw API response stored in solax/raw/.
type DayRecord struct {
	Date string `json:"date"` // YYYY-MM-DD

	// Daily totals (kWh)
	TotalYield       float64 `json:"total_yield"`
	FeedIn           float64 `json:"feed_in"`
	GridImport       float64 `json:"grid_import"`
	BatteryCharge    float64 `json:"battery_charge"`
	BatteryDischarge float64 `json:"battery_discharge"`
	TotalLoad        float64 `json:"total_load"`

	// 5-minute resolution timeseries (288 slots per day).
	// Nil yais values from the API are represented as NaN.
	PVPower      []float64 `json:"pv_power"`      // kW
	LoadPower    []float64 `json:"load_power"`    // kW
	BatteryPower []float64 `json:"battery_power"` // kW, positive = charging
	BatterySoC   []float64 `json:"battery_soc"`   // %
	FeedInPower  []float64 `json:"feed_in_power"` // kW, positive = export, negative = import
}

// Raw is the top-level structure of a raw API response file.
type Raw struct {
	SiteFlag   int    `json:"siteFlag"`
	HasBattery int    `json:"hasBattery"`
	DataTime   string `json:"dataTime"`

	Yield struct {
		TotalYield float64 `json:"totalYield"`
		Load       float64 `json:"load"`
		Grid       float64 `json:"grid"`
		Battery    float64 `json:"battery"`
	} `json:"yield"`

	Consumed struct {
		TotalConsumed float64 `json:"totalConsumed"`
		Grid          float64 `json:"grid"`
		Battery       float64 `json:"battery"`
	} `json:"consumed"`

	ChargeYield    float64 `json:"chargeYield"`
	DisChargeYield float64 `json:"disChargeYield"`
	FeedInEnergy   float64 `json:"feedInEnergy"`
	ConsumeEnergy  float64 `json:"consumeEnergy"`

	Records struct {
		PVPower      []DataPoint `json:"pvPower"`
		LoadPower    []DataPoint `json:"loadPower"`
		BatteryPower []DataPoint `json:"batteryPower"`
		BatterySoC   []DataPoint `json:"batterySoc"`
		FeedInPower  []DataPoint `json:"feedInPower"`
	} `json:"records"`
}

// DataPoint is a single 5-minute data point. Yais is nil when no data was recorded.
type DataPoint struct {
	Xais string   `json:"xais"` // "HH:MM"
	Yais *float64 `json:"yais"`
}

// UnmarshalJSON handles the API returning yais as null, a number, or a numeric string.
func (dp *DataPoint) UnmarshalJSON(b []byte) error {
	var raw struct {
		Xais string          `json:"xais"`
		Yais json.RawMessage `json:"yais"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	dp.Xais = raw.Xais

	if len(raw.Yais) == 0 || string(raw.Yais) == "null" {
		dp.Yais = nil
		return nil
	}

	// Try number first
	var f float64
	if err := json.Unmarshal(raw.Yais, &f); err == nil {
		dp.Yais = &f
		return nil
	}

	// Fall back to quoted string (e.g. "1.5")
	var s string
	if err := json.Unmarshal(raw.Yais, &s); err == nil {
		if s == "" {
			dp.Yais = nil
			return nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("yais: cannot parse %q as float: %w", s, err)
		}
		dp.Yais = &f
		return nil
	}

	return fmt.Errorf("yais: unexpected value %s", raw.Yais)
}

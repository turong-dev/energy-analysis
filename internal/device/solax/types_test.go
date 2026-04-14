package solax

import (
	"encoding/json"
	"math"
	"testing"
)

func TestDataPointUnmarshalNull(t *testing.T) {
	data := `{"xais":"12:00","yais":null}`
	var dp DataPoint
	if err := json.Unmarshal([]byte(data), &dp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dp.Xais != "12:00" {
		t.Errorf("Xais = %q, want 12:00", dp.Xais)
	}
	if dp.Yais != nil {
		t.Errorf("Yais = %v, want nil", dp.Yais)
	}
}

func TestDataPointUnmarshalNumber(t *testing.T) {
	data := `{"xais":"12:00","yais":1.5}`
	var dp DataPoint
	if err := json.Unmarshal([]byte(data), &dp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if *dp.Yais != 1.5 {
		t.Errorf("Yais = %v, want 1.5", *dp.Yais)
	}
}

func TestDataPointUnmarshalQuotedNumber(t *testing.T) {
	data := `{"xais":"12:00","yais":"2.75"}`
	var dp DataPoint
	if err := json.Unmarshal([]byte(data), &dp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if *dp.Yais != 2.75 {
		t.Errorf("Yais = %v, want 2.75", *dp.Yais)
	}
}

func TestDataPointUnmarshalEmptyString(t *testing.T) {
	data := `{"xais":"12:00","yais":""}`
	var dp DataPoint
	if err := json.Unmarshal([]byte(data), &dp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dp.Yais != nil {
		t.Errorf("Yais = %v, want nil for empty string", dp.Yais)
	}
}

func TestDataPointUnmarshalZero(t *testing.T) {
	data := `{"xais":"12:00","yais":0}`
	var dp DataPoint
	if err := json.Unmarshal([]byte(data), &dp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dp.Yais == nil {
		t.Fatal("Yais should not be nil for zero")
	}
	if *dp.Yais != 0 {
		t.Errorf("Yais = %v, want 0", *dp.Yais)
	}
}

func TestParseRawBasic(t *testing.T) {
	pv := func(v float64) *float64 { return &v }
	raw := &Raw{
		Yield: struct {
			TotalYield float64 `json:"totalYield"`
			Load       float64 `json:"load"`
			Grid       float64 `json:"grid"`
			Battery    float64 `json:"battery"`
		}{
			TotalYield: 12.5,
		},
		Consumed: struct {
			TotalConsumed float64 `json:"totalConsumed"`
			Grid          float64 `json:"grid"`
			Battery       float64 `json:"battery"`
		}{
			Grid: 5.0,
		},
		ChargeYield:    3.2,
		DisChargeYield: 1.8,
		FeedInEnergy:   4.1,
		ConsumeEnergy:  12.0,
		Records: struct {
			PVPower      []DataPoint `json:"pvPower"`
			LoadPower    []DataPoint `json:"loadPower"`
			BatteryPower []DataPoint `json:"batteryPower"`
			BatterySoC   []DataPoint `json:"batterySoc"`
			FeedInPower  []DataPoint `json:"feedInPower"`
		}{
			PVPower:      []DataPoint{{Xais: "12:00", Yais: pv(1.5)}},
			LoadPower:    []DataPoint{{Xais: "12:00", Yais: pv(2.0)}},
			BatteryPower: []DataPoint{{Xais: "12:00", Yais: pv(0.5)}},
			BatterySoC:   []DataPoint{{Xais: "12:00", Yais: pv(80.0)}},
			FeedInPower:  []DataPoint{{Xais: "12:00", Yais: nil}},
		},
	}

	rec := ParseRaw(raw, "2025-04-15")

	if rec.Date != "2025-04-15" {
		t.Errorf("Date = %q, want 2025-04-15", rec.Date)
	}
	if rec.TotalYield != 12.5 {
		t.Errorf("TotalYield = %v, want 12.5", rec.TotalYield)
	}
	if rec.FeedIn != 4.1 {
		t.Errorf("FeedIn = %v, want 4.1", rec.FeedIn)
	}
	if rec.GridImport != 5.0 {
		t.Errorf("GridImport = %v, want 5.0", rec.GridImport)
	}
	if rec.BatteryCharge != 3.2 {
		t.Errorf("BatteryCharge = %v, want 3.2", rec.BatteryCharge)
	}
	if rec.BatteryDischarge != 1.8 {
		t.Errorf("BatteryDischarge = %v, want 1.8", rec.BatteryDischarge)
	}
	if rec.TotalLoad != 12.0 {
		t.Errorf("TotalLoad = %v, want 12.0", rec.TotalLoad)
	}

	if len(rec.PVPower) != 1 || rec.PVPower[0] != 1.5 {
		t.Errorf("PVPower = %v, want [1.5]", rec.PVPower)
	}
	if len(rec.BatterySoC) != 1 || rec.BatterySoC[0] != 80.0 {
		t.Errorf("BatterySoC = %v, want [80.0]", rec.BatterySoC)
	}
	if len(rec.FeedInPower) != 1 || !math.IsNaN(rec.FeedInPower[0]) {
		t.Errorf("FeedInPower = %v, want [NaN]", rec.FeedInPower)
	}
}

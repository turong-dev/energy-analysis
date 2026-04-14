package analysis

import (
	"math"
	"testing"
	"time"

	"energy-utility/internal/device"
)

func TestDaytimeChargeKWhFromModeSwitch(t *testing.T) {
	fiveMin := 5 * time.Minute
	power := make([]float64, 288)
	for i := range power {
		power[i] = math.NaN()
	}
	for i := 84; i < 96; i++ {
		power[i] = 2.0
	}

	d := device.DayData{
		Date:         time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		Resolution:   fiveMin,
		TotalYield:   10.0,
		BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power},
	}

	kwh := daytimeChargeKWh(d)
	expected := 2.0 * 12 * (5.0 / 60.0)
	if math.Abs(kwh-expected) > 0.01 {
		t.Errorf("daytimeChargeKWh = %v, want %v", kwh, expected)
	}
}

func TestDaytimeChargeKWhIgnoresPre7am(t *testing.T) {
	fiveMin := 5 * time.Minute
	power := make([]float64, 288)
	for i := range power {
		power[i] = math.NaN()
	}
	for i := 0; i < 84; i++ {
		power[i] = 3.0
	}

	d := device.DayData{
		Date:         time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		Resolution:   fiveMin,
		TotalYield:   10.0,
		BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power},
	}

	kwh := daytimeChargeKWh(d)
	if kwh != 0 {
		t.Errorf("daytimeChargeKWh = %v, want 0 (all charging before 7am)", kwh)
	}
}

func TestDaytimeChargeKWhIgnoresNegativeDischarge(t *testing.T) {
	fiveMin := 5 * time.Minute
	power := make([]float64, 288)
	for i := range power {
		power[i] = math.NaN()
	}
	for i := 84; i < 96; i++ {
		power[i] = -1.5
	}

	d := device.DayData{
		Date:         time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
		Resolution:   fiveMin,
		TotalYield:   10.0,
		BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power},
	}

	kwh := daytimeChargeKWh(d)
	if kwh != 0 {
		t.Errorf("daytimeChargeKWh = %v, want 0 (discharging should not count)", kwh)
	}
}

func TestFindSwitchDateTooFewSignals(t *testing.T) {
	signals := []DaySignal{
		{Date: "2024-01-01", DaytimeChargeKWh: 0.1, TotalYieldKWh: 5.0},
		{Date: "2024-01-02", DaytimeChargeKWh: 0.2, TotalYieldKWh: 6.0},
	}
	result := findSwitchDate(signals)
	if result != nil {
		t.Errorf("findSwitchDate with < 20 signals should return nil, got %v", *result)
	}
}

func TestFindSwitchDateNoClearTransition(t *testing.T) {
	signals := make([]DaySignal, 30)
	for i := range signals {
		signals[i] = DaySignal{
			Date:             "2024-01-01",
			DaytimeChargeKWh: 0.1,
			TotalYieldKWh:    5.0,
		}
	}
	result := findSwitchDate(signals)
	if result != nil {
		t.Errorf("findSwitchDate with consistent low charging should return nil, got %v", *result)
	}
}

func TestDetectModeSwitchLowYieldFiltered(t *testing.T) {
	power := make([]float64, 288)

	days := []device.DayData{
		{
			Date:         time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			Resolution:   5 * time.Minute,
			TotalYield:   0.5,
			BatteryPower: device.TimeSeries{Resolution: 5 * time.Minute, Values: power},
		},
	}

	result := DetectModeSwitch(days)
	for _, d := range result.Days {
		if d.TotalYieldKWh < 1.0 {
			t.Errorf("days with yield < 1.0 kWh should be filtered, got %v", d.TotalYieldKWh)
		}
	}
}

func TestFindSwitchDateWithClearTransition(t *testing.T) {
	signals := make([]DaySignal, 40)
	for i := 0; i < 20; i++ {
		signals[i] = DaySignal{
			Date:             "2024-01-01",
			DaytimeChargeKWh: 0.1,
			TotalYieldKWh:    5.0,
		}
	}
	for i := 20; i < 40; i++ {
		signals[i] = DaySignal{
			Date:             "2024-02-01",
			DaytimeChargeKWh: 2.0,
			TotalYieldKWh:    10.0,
		}
	}
	result := findSwitchDate(signals)
	if result == nil {
		t.Fatal("expected a switch date to be found")
	}
}

func TestDetectModeSwitchSorted(t *testing.T) {
	fiveMin := 5 * time.Minute
	power := make([]float64, 288)
	for i := 84; i < 200; i++ {
		power[i] = 2.0
	}

	days := []device.DayData{
		{Date: time.Date(2024, 8, 15, 0, 0, 0, 0, time.UTC), Resolution: fiveMin, TotalYield: 10.0, BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power}},
		{Date: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), Resolution: fiveMin, TotalYield: 8.0, BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power}},
		{Date: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC), Resolution: fiveMin, TotalYield: 9.0, BatteryPower: device.TimeSeries{Resolution: fiveMin, Values: power}},
	}

	result := DetectModeSwitch(days)
	for i := 1; i < len(result.Days); i++ {
		if result.Days[i].Date < result.Days[i-1].Date {
			t.Errorf("days not sorted: %s > %s", result.Days[i-1].Date, result.Days[i].Date)
		}
	}
}

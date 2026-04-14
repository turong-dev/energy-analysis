package analysis

import (
	"math"
	"testing"
	"time"

	"energy-utility/internal/device"
	"energy-utility/internal/device/fake"
)

func TestAnalyseChargingWithFakeInverter(t *testing.T) {
	inv := fake.NewInverter()

	soc := make([]float64, 288)
	for i := range soc {
		if i >= 84 && i < 200 {
			soc[i] = 80.0
		} else {
			soc[i] = 100.0
		}
	}

	inv.AddDay(fake.MakeDayData("2024-06-15", 15.0, soc))

	days := inv.Data
	result := AnalyseCharging(days)

	if len(result.Days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(result.Days))
	}

	d := result.Days[0]
	if d.Date != "2024-06-15" {
		t.Errorf("Date = %q, want 2024-06-15", d.Date)
	}
	if d.TotalYieldKWh != 15.0 {
		t.Errorf("TotalYieldKWh = %v, want 15.0", d.TotalYieldKWh)
	}
	if d.MinSoC != 80.0 {
		t.Errorf("MinSoC = %v, want 80.0", d.MinSoC)
	}
	if d.Season != "summer" {
		t.Errorf("Season = %q, want summer", d.Season)
	}
}

func TestAnalyseChargingDepletedWithFakeInverter(t *testing.T) {
	inv := fake.NewInverter()

	soc := make([]float64, 288)
	for i := range soc {
		if i >= 84 {
			soc[i] = 10.0
		} else {
			soc[i] = 100.0
		}
	}

	inv.AddDay(fake.MakeDayData("2024-01-15", 5.0, soc))

	result := AnalyseCharging(inv.Data)

	if len(result.Days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(result.Days))
	}

	d := result.Days[0]
	if !d.Depleted {
		t.Error("should be marked as depleted at 10% SoC")
	}
	if d.Season != "winter" {
		t.Errorf("Season = %q, want winter", d.Season)
	}
}

func TestDetectModeSwitchWithFakeInverter(t *testing.T) {
	inv := fake.NewInverter()

	soc := make([]float64, 288)
	for i := range soc {
		soc[i] = 50.0
	}

	power := make([]float64, 288)
	for i := 84; i < 288; i++ {
		power[i] = 2.0
	}

	for _, date := range []string{"2024-01-01", "2024-01-02", "2024-01-03", "2024-01-04", "2024-01-05"} {
		d := fake.MakeDayData(date, 10.0, soc)
		d.BatteryPower = device.TimeSeries{Resolution: 5 * time.Minute, Values: power}
		inv.AddDay(d)
	}

	result := DetectModeSwitch(inv.Data)

	if len(result.Days) != 5 {
		t.Errorf("expected 5 days, got %d", len(result.Days))
	}
}

func TestAnalyseChargingFiltersShortSoc(t *testing.T) {
	inv := fake.NewInverter()

	soc := make([]float64, 10)
	for i := range soc {
		soc[i] = 50.0
	}

	inv.AddDay(fake.MakeDayData("2024-06-15", 15.0, soc))

	result := AnalyseCharging(inv.Data)

	if len(result.Days) != 0 {
		t.Error("days with < 100 SoC slots should be filtered out")
	}
}

func TestAnalyseChargingFiltersLowYield(t *testing.T) {
	inv := fake.NewInverter()

	soc := make([]float64, 288)
	for i := range soc {
		soc[i] = 50.0
	}

	inv.AddDay(fake.MakeDayData("2024-06-15", 0.5, soc))

	result := DetectModeSwitch(inv.Data)

	if len(result.Days) != 0 {
		t.Error("days with yield < 1.0 kWh should be filtered in DetectModeSwitch")
	}
}

func TestChargingBreakevenWithFakeData(t *testing.T) {
	days := []DayCharging{
		{TotalYieldKWh: 5.0, Depleted: true},
		{TotalYieldKWh: 6.0, Depleted: true},
		{TotalYieldKWh: 7.0, Depleted: true},
		{TotalYieldKWh: 15.0, Depleted: false},
		{TotalYieldKWh: 20.0, Depleted: false},
		{TotalYieldKWh: 25.0, Depleted: false},
	}

	threshold := chargingBreakeven(days)

	if threshold < 7.0 || threshold > 15.0 {
		t.Errorf("breakeven = %v, expected between 7 and 15", threshold)
	}
}

func TestChargingSeasons(t *testing.T) {
	days := []DayCharging{
		{Date: "2024-06-15", TotalYieldKWh: 15.0, Depleted: false, Season: "summer"},
		{Date: "2024-06-20", TotalYieldKWh: 12.0, Depleted: true, Season: "summer"},
		{Date: "2024-12-15", TotalYieldKWh: 5.0, Depleted: true, Season: "winter"},
		{Date: "2024-12-20", TotalYieldKWh: 3.0, Depleted: true, Season: "winter"},
	}

	seasons := chargingSeasons(days)

	if len(seasons) != 2 {
		t.Errorf("expected 2 seasons, got %d", len(seasons))
	}

	summer := seasons["summer"]
	if summer.TotalDays != 2 {
		t.Errorf("summer total days = %d, want 2", summer.TotalDays)
	}
	if summer.DepletedDays != 1 {
		t.Errorf("summer depleted days = %d, want 1", summer.DepletedDays)
	}

	winter := seasons["winter"]
	if winter.TotalDays != 2 {
		t.Errorf("winter total days = %d, want 2", winter.TotalDays)
	}
	if winter.DepletedDays != 2 {
		t.Errorf("winter depleted days = %d, want 2", winter.DepletedDays)
	}
}

func TestDaytimeChargeKWhWithDifferentResolutions(t *testing.T) {
	tests := []struct {
		name       string
		resolution time.Duration
		slots      int
		want       float64
	}{
		{
			name:       "5-minute resolution",
			resolution: 5 * time.Minute,
			slots:      288,
			want:       2.0 * 12 * (5.0 / 60.0),
		},
		{
			name:       "1-minute resolution",
			resolution: 1 * time.Minute,
			slots:      1440,
			want:       2.0 * 12 * (1.0 / 60.0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			power := make([]float64, tt.slots)
			for i := range power {
				power[i] = math.NaN()
			}
			startSlot := 7 * 60 / int(tt.resolution.Minutes())
			for i := startSlot; i < startSlot+12; i++ {
				power[i] = 2.0
			}

			d := device.DayData{
				Date:         time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC),
				Resolution:   tt.resolution,
				BatteryPower: device.TimeSeries{Resolution: tt.resolution, Values: power},
			}

			got := daytimeChargeKWh(d)
			if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("daytimeChargeKWh = %v, want %v", got, tt.want)
			}
		})
	}
}

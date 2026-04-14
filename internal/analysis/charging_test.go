package analysis

import (
	"math"
	"testing"
	"time"

	"energy-utility/internal/solax"
)

func TestComputeMinSoC(t *testing.T) {
	tests := []struct {
		name string
		soc  []float64
		want float64
	}{
		{
			name: "normal day",
			soc: func() []float64 {
				s := make([]float64, 288)
				for i := range s {
					s[i] = 100 - float64(i)*0.2
				}
				return s
			}(),
			want: 42.6,
		},
		{
			name: "NaN values ignored",
			soc: func() []float64 {
				s := make([]float64, 288)
				for i := range s {
					s[i] = math.NaN()
				}
				s[90] = 50.0
				s[91] = 60.0
				return s
			}(),
			want: 50.0,
		},
		{
			name: "too short returns NaN",
			soc:  make([]float64, 50),
			want: math.NaN(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeMinSoC(tt.soc)
			if math.IsNaN(tt.want) {
				if !math.IsNaN(got) {
					t.Errorf("computeMinSoC = %v, want NaN", got)
				}
			} else if math.Abs(got-tt.want) > 0.01 {
				t.Errorf("computeMinSoC = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSeasonFor(t *testing.T) {
	tests := []struct {
		month int
		want  string
	}{
		{int(time.January), "winter"},
		{int(time.February), "winter"},
		{int(time.March), "spring"},
		{int(time.April), "spring"},
		{int(time.May), "spring"},
		{int(time.June), "summer"},
		{int(time.July), "summer"},
		{int(time.August), "summer"},
		{int(time.September), "autumn"},
		{int(time.October), "autumn"},
		{int(time.November), "autumn"},
		{int(time.December), "winter"},
	}
	for _, tt := range tests {
		d := time.Date(2024, time.Month(tt.month), 15, 0, 0, 0, 0, time.UTC)
		if got := seasonFor(d); got != tt.want {
			t.Errorf("seasonFor(month=%d) = %q, want %q", tt.month, got, tt.want)
		}
	}
}

func TestAnalyseChargingFiltersLowBatteryCount(t *testing.T) {
	days := []solax.DayRecord{
		{Date: "2024-06-01", BatterySoC: make([]float64, 10)},
	}
	result := AnalyseCharging(days)
	if len(result.Days) != 0 {
		t.Error("days with < 100 SoC slots should be filtered out")
	}
}

func TestAnalyseChargingNormal(t *testing.T) {
	soc := make([]float64, 288)
	for i := range soc {
		soc[i] = math.NaN()
	}
	for i := 84; i < 200; i++ {
		soc[i] = 100 - float64(i-84)*0.5
	}

	power := make([]float64, 288)
	for i := range power {
		power[i] = math.NaN()
	}
	for i := 84; i < 200; i++ {
		power[i] = 2.0 // 2 kW charging
	}

	days := []solax.DayRecord{
		{
			Date:         "2024-06-15",
			TotalYield:   15.0,
			BatterySoC:   soc,
			BatteryPower: power,
		},
	}

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
	if d.Depleted {
		t.Error("should not be depleted at min SoC > 15%")
	}
	if d.Season != "summer" {
		t.Errorf("Season = %q, want summer", d.Season)
	}
}

func TestDaytimeChargeKWh(t *testing.T) {
	power := make([]float64, 288)
	for i := range power {
		power[i] = math.NaN()
	}
	for i := 84; i < 96; i++ {
		power[i] = 2.0
	}

	d := solax.DayRecord{
		Date:         "2024-06-15",
		TotalYield:   10.0,
		BatterySoC:   make([]float64, 288),
		BatteryPower: power,
	}

	kwh := daytimeChargeKWh(d)
	expected := 2.0 * 12 * (5.0 / 60.0)
	if math.Abs(kwh-expected) > 0.01 {
		t.Errorf("daytimeChargeKWh = %v, want %v", kwh, expected)
	}
}

func TestChargingBreakevenSmallDataset(t *testing.T) {
	if got := chargingBreakeven(nil); got != 0 {
		t.Error("breakeven nil input should be 0")
	}
	if got := chargingBreakeven([]DayCharging{{}, {}}); got != 0 {
		t.Error("breakeven all-same Depleted status should be 0")
	}
}

func TestChargingBreakevenPerfectSplit(t *testing.T) {
	days := []DayCharging{
		{TotalYieldKWh: 5.0, Depleted: true},
		{TotalYieldKWh: 6.0, Depleted: true},
		{TotalYieldKWh: 15.0, Depleted: false},
		{TotalYieldKWh: 20.0, Depleted: false},
	}
	threshold := chargingBreakeven(days)
	if threshold < 6.0 || threshold > 15.0 {
		t.Errorf("breakeven = %v, expected between 6 and 15", threshold)
	}
}

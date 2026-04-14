package config

import (
	"testing"
	"time"
)

func TestGoRateAt(t *testing.T) {
	cfg := OctopusConfig{
		GoRates: []GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "00:30",
				OffpeakEnd:     "04:30",
				StandingCharge: 25.0,
			},
			{
				From:           "2024-07-01",
				PeakRate:       31.0,
				OffpeakRate:    8.0,
				OffpeakStart:   "00:00",
				OffpeakEnd:     "00:00",
				StandingCharge: 26.0,
			},
		},
	}

	tests := []struct {
		date     string
		peak     float64
		offpeak  float64
		standing float64
		found    bool
	}{
		{"2024-03-15", 30.0, 7.5, 25.0, true},
		{"2024-08-01", 31.0, 8.0, 26.0, true},
		{"2023-06-01", 0, 0, 0, false},
	}

	for _, tt := range tests {
		d, _ := time.Parse("2006-01-02", tt.date)
		r := cfg.GoRateAt(d)
		if tt.found {
			if r == nil {
				t.Errorf("GoRateAt(%s) = nil, want a rate", tt.date)
				continue
			}
			if r.PeakRate != tt.peak {
				t.Errorf("GoRateAt(%s).PeakRate = %v, want %v", tt.date, r.PeakRate, tt.peak)
			}
			if r.OffpeakRate != tt.offpeak {
				t.Errorf("GoRateAt(%s).OffpeakRate = %v, want %v", tt.date, r.OffpeakRate, tt.offpeak)
			}
			if r.StandingCharge != tt.standing {
				t.Errorf("GoRateAt(%s).StandingCharge = %v, want %v", tt.date, r.StandingCharge, tt.standing)
			}
		} else {
			if r != nil {
				t.Errorf("GoRateAt(%s) = %+v, want nil", tt.date, r)
			}
		}
	}
}

func TestGoRateAtPicksMostRecent(t *testing.T) {
	cfg := OctopusConfig{
		GoRates: []GoRate{
			{From: "2024-01-01", PeakRate: 28.0, OffpeakRate: 7.0, StandingCharge: 24.0},
			{From: "2024-06-01", PeakRate: 30.0, OffpeakRate: 7.5, StandingCharge: 25.0},
		},
	}

	d, _ := time.Parse("2006-01-02", "2024-07-01")
	r := cfg.GoRateAt(d)
	if r == nil {
		t.Fatal("expected a rate")
	}
	if r.PeakRate != 30.0 {
		t.Errorf("GoRateAt should pick the most recent rate, got PeakRate=%v", r.PeakRate)
	}
}

func TestExportRateAt(t *testing.T) {
	cfg := OctopusConfig{
		ExportRates: []ExportRate{
			{From: "2024-01-01", Rate: 4.1},
			{From: "2024-07-01", Rate: 15.0},
		},
	}

	tests := []struct {
		date string
		want float64
	}{
		{"2024-03-01", 4.1},
		{"2024-08-01", 15.0},
		{"2023-06-01", 0},
	}

	for _, tt := range tests {
		d, _ := time.Parse("2006-01-02", tt.date)
		got := cfg.ExportRateAt(d)
		if got != tt.want {
			t.Errorf("ExportRateAt(%s) = %v, want %v", tt.date, got, tt.want)
		}
	}
}

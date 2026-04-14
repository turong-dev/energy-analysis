package analysis

import (
	"math"
	"testing"
	"time"
	_ "time/tzdata"

	"energy-utility/internal/config"
	"energy-utility/internal/octopus"
)

func londonTime(date, hhmm string) time.Time {
	loc, _ := time.LoadLocation("Europe/London")
	t, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, loc)
	if err != nil {
		panic(err)
	}
	return t
}

func utcTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func floatPtr(v float64) *float64 { return &v }

func TestGoRateForSlot(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "00:30",
				OffpeakEnd:     "04:30",
				StandingCharge: 25.0,
			},
		},
	}

	tests := []struct {
		name string
		time string
		want float64
	}{
		{"midnight offpeak (01:00 BST)", "2024-06-15T00:00:00Z", 7.5},
		{"offpeak start", "2024-06-15T00:30:00+01:00", 7.5},
		{"mid offpeak", "2024-06-15T02:00:00+01:00", 7.5},
		{"offpeak end exclusive", "2024-06-15T04:30:00+01:00", 30.0},
		{"daytime peak", "2024-06-15T12:00:00+01:00", 30.0},
	}

	for _, tt := range tests {
		slotTime, _ := time.Parse(time.RFC3339, tt.time)
		got := goRateForSlot(slotTime, cfg)
		if math.Abs(got-tt.want) > 0.01 {
			t.Errorf("goRateForSlot(%s) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestGoRateForSlotCrossMidnight(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "23:30",
				OffpeakEnd:     "05:30",
				StandingCharge: 25.0,
			},
		},
	}

	tests := []struct {
		name string
		time string
		want float64
	}{
		{"just before offpeak", "2024-06-15T23:29:00+01:00", 30.0},
		{"offpeak starts 23:30", "2024-06-15T23:30:00+01:00", 7.5},
		{"midnight offpeak", "2024-06-16T00:00:00+01:00", 7.5},
		{"offpeak continues", "2024-06-16T03:00:00+01:00", 7.5},
		{"offpeak ends at 05:30", "2024-06-16T05:30:00+01:00", 30.0},
	}

	for _, tt := range tests {
		slotTime, _ := time.Parse(time.RFC3339, tt.time)
		got := goRateForSlot(slotTime, cfg)
		if math.Abs(got-tt.want) > 0.01 {
			t.Errorf("%s: goRateForSlot = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestGoRateForSlotAllDayOffpeak(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       1.0,
				OffpeakRate:    0.0,
				OffpeakStart:   "00:00",
				OffpeakEnd:     "00:00",
				StandingCharge: 0,
			},
		},
	}

	slot := londonTime("2024-06-15", "12:00")
	got := goRateForSlot(slot, cfg)
	if got != 0.0 {
		t.Errorf("all-day offpeak at noon: got %v, want 0", got)
	}
}

func TestBuildRateMap(t *testing.T) {
	rates := []octopus.HalfHourlyRate{
		{ValidFrom: utcTime("2024-06-15T12:00:00Z"), ValueIncVAT: 25.0},
		{ValidFrom: utcTime("2024-06-15T12:30:00Z"), ValueIncVAT: 30.0},
	}
	m := buildRateMap(rates)
	if len(m) != 2 {
		t.Fatalf("rate map length = %d, want 2", len(m))
	}
	if m[utcTime("2024-06-15T12:00:00Z")] != 25.0 {
		t.Error("first rate mismatch")
	}
	if m[utcTime("2024-06-15T12:30:00Z")] != 30.0 {
		t.Error("second rate mismatch")
	}
	if _, ok := m[utcTime("2024-06-15T13:00:00Z")]; ok {
		t.Error("should not have rate at 13:00")
	}
}

func TestAgreementsToPeriods(t *testing.T) {
	validTo := mustParseTime("2024-06-01T00:00:00Z")
	agreements := []octopus.TariffAgreement{
		{TariffCode: "E-1R-VAR-22-11-01-A", ValidFrom: mustParseTime("2024-01-01T00:00:00Z"), ValidTo: &validTo},
		{TariffCode: "E-1R-AGILE-24-10-01-E", ValidFrom: mustParseTime("2024-06-01T00:00:00Z"), ValidTo: nil},
	}

	periods := AgreementsToPeriods(agreements)
	if len(periods) != 2 {
		t.Fatalf("got %d periods, want 2", len(periods))
	}
	if periods[0].Tariff != "Unknown" {
		t.Errorf("first period tariff = %q, want Unknown", periods[0].Tariff)
	}
	if periods[0].To == nil {
		t.Error("first period should have a To date")
	}
	if periods[1].Tariff != "Agile" {
		t.Errorf("second period tariff = %q, want Agile", periods[1].Tariff)
	}
	if periods[1].To != nil {
		t.Error("second period To should be nil (ongoing)")
	}
}

func TestAgreementsToPeriodsGoTariff(t *testing.T) {
	agreements := []octopus.TariffAgreement{
		{TariffCode: "E-1R-GO-VAR-22-10-14-E", ValidFrom: mustParseTime("2024-01-01T00:00:00Z"), ValidTo: nil},
	}
	periods := AgreementsToPeriods(agreements)
	if len(periods) != 1 {
		t.Fatalf("got %d periods, want 1", len(periods))
	}
	if periods[0].Tariff != "Go" {
		t.Errorf("tariff = %q, want Go", periods[0].Tariff)
	}
}

func TestActualImportRate(t *testing.T) {
	agileAgreement := octopus.TariffAgreement{
		TariffCode: "E-1R-AGILE-24-10-01-E",
		ValidFrom:  mustParseTime("2024-06-01T00:00:00Z"),
		ValidTo:    nil,
	}
	goAgreement := octopus.TariffAgreement{
		TariffCode: "E-1R-GO-VAR-22-10-14-E",
		ValidFrom:  mustParseTime("2024-01-01T00:00:00Z"),
		ValidTo:    nil,
	}

	slotTime := mustParseTime("2024-07-01T12:00:00Z")

	if got := actualImportRate(slotTime, []octopus.TariffAgreement{agileAgreement}, 20.0, 30.0); got != 20.0 {
		t.Errorf("actualImportRate with agile agreement = %v, want 20.0", got)
	}
	if got := actualImportRate(slotTime, []octopus.TariffAgreement{goAgreement}, 20.0, 30.0); got != 30.0 {
		t.Errorf("actualImportRate with go agreement = %v, want 30.0", got)
	}
	if got := actualImportRate(slotTime, nil, 20.0, 30.0); got != 30.0 {
		t.Errorf("actualImportRate with no agreement = %v, want 30.0 (fallback to goRate)", got)
	}
}

func TestActualExportRate(t *testing.T) {
	agileAgreement := octopus.TariffAgreement{
		TariffCode: "E-1R-AGILE-OUTGOING-24-10-01-E",
		ValidFrom:  mustParseTime("2024-06-01T00:00:00Z"),
		ValidTo:    nil,
	}
	slotTime := mustParseTime("2024-07-01T12:00:00Z")

	if got := actualExportRate(slotTime, []octopus.TariffAgreement{agileAgreement}, 15.0, 4.1); got != 15.0 {
		t.Errorf("actualExportRate with agile = %v, want 15.0", got)
	}
	if got := actualExportRate(slotTime, nil, 15.0, 4.1); got != 4.1 {
		t.Errorf("actualExportRate with no agreement = %v, want 4.1 (fallback fixedRate)", got)
	}
}

func TestCalculate(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "00:30",
				OffpeakEnd:     "04:30",
				StandingCharge: 25.0,
			},
		},
		ExportRates: []config.ExportRate{
			{From: "2024-01-01", Rate: 4.1},
		},
	}

	importRates := []octopus.HalfHourlyRate{
		{ValidFrom: utcTime("2024-06-15T12:00:00Z"), ValueIncVAT: 20.0},
	}
	exportRates := []octopus.HalfHourlyRate{
		{ValidFrom: utcTime("2024-06-15T12:00:00Z"), ValueIncVAT: 15.0},
	}
	importConsumption := []octopus.HalfHourlyConsumption{
		{IntervalStart: utcTime("2024-06-15T12:00:00Z"), IntervalEnd: utcTime("2024-06-15T12:30:00Z"), Consumption: 0.5},
	}
	exportConsumption := []octopus.HalfHourlyConsumption{
		{IntervalStart: utcTime("2024-06-15T12:00:00Z"), IntervalEnd: utcTime("2024-06-15T12:30:00Z"), Consumption: 0.3},
	}

	result := Calculate(importRates, exportRates, importConsumption, exportConsumption, nil, nil, cfg, 27.0)

	if len(result.Days) != 1 {
		t.Fatalf("expected 1 day, got %d", len(result.Days))
	}
	d := result.Days[0]
	if d.ImportKWh != 0.5 {
		t.Errorf("ImportKWh = %v, want 0.5", d.ImportKWh)
	}
	if d.ExportKWh != 0.3 {
		t.Errorf("ExportKWh = %v, want 0.3", d.ExportKWh)
	}
	if d.AgileImport != 0.5*20.0 {
		t.Errorf("AgileImport = %v, want %v", d.AgileImport, 0.5*20.0)
	}
	if d.AgileExport != 0.3*15.0 {
		t.Errorf("AgileExport = %v, want %v", d.AgileExport, 0.3*15.0)
	}
	if d.GoStandingCharge != 25.0 {
		t.Errorf("GoStandingCharge = %v, want 25.0", d.GoStandingCharge)
	}
	if d.AgileStandingCharge != 27.0 {
		t.Errorf("AgileStandingCharge = %v, want 27.0", d.AgileStandingCharge)
	}
	if d.ActualStandingCharge != 25.0 {
		t.Errorf("ActualStandingCharge = %v, want 25.0 (Go fallback with no agreements)", d.ActualStandingCharge)
	}
}

func TestCalculateWithAgileAgreement(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "00:30",
				OffpeakEnd:     "04:30",
				StandingCharge: 25.0,
			},
		},
		ExportRates: []config.ExportRate{
			{From: "2024-01-01", Rate: 4.1},
		},
	}

	agileFrom := mustParseTime("2024-06-01T00:00:00Z")
	importAgreements := []octopus.TariffAgreement{
		{TariffCode: "E-1R-AGILE-24-10-01-E", ValidFrom: agileFrom, ValidTo: nil},
	}

	slot := utcTime("2024-06-15T12:00:00Z")
	importRates := []octopus.HalfHourlyRate{
		{ValidFrom: slot, ValueIncVAT: 20.0},
	}
	exportRates := []octopus.HalfHourlyRate{
		{ValidFrom: slot, ValueIncVAT: 15.0},
	}
	importConsumption := []octopus.HalfHourlyConsumption{
		{IntervalStart: slot, Consumption: 1.0},
	}
	exportConsumption := []octopus.HalfHourlyConsumption{
		{IntervalStart: slot, Consumption: 0.5},
	}

	result := Calculate(importRates, exportRates, importConsumption, exportConsumption, importAgreements, nil, cfg, 27.0)

	d := result.Days[0]
	if d.ActualImport != 20.0 {
		t.Errorf("ActualImport = %v, want 20.0 (agile rate)", d.ActualImport)
	}
	if d.ActualExport != 0.5*4.1 {
		t.Errorf("ActualExport = %v, want %v (go fixed rate × export kWh)", d.ActualExport, 0.5*4.1)
	}
	if d.ActualStandingCharge != 27.0 {
		t.Errorf("ActualStandingCharge = %v, want 27.0 (agile)", d.ActualStandingCharge)
	}
}

func TestIsAgileDay(t *testing.T) {
	agileFrom := mustParseTime("2024-06-01T00:00:00Z")
	agreements := []octopus.TariffAgreement{
		{TariffCode: "E-1R-AGILE-24-10-01-E", ValidFrom: agileFrom, ValidTo: nil},
	}

	if !isAgileDay("2024-07-15", agreements) {
		t.Error("day during agile agreement should be agile")
	}
	if isAgileDay("2024-05-15", agreements) {
		t.Error("day before agile agreement should not be agile")
	}
	if isAgileDay("2024-07-15", nil) {
		t.Error("no agreements should not be agile")
	}
}

func TestStandingChargeForDay(t *testing.T) {
	cfg := &config.OctopusConfig{
		GoRates: []config.GoRate{
			{
				From:           "2024-01-01",
				PeakRate:       30.0,
				OffpeakRate:    7.5,
				OffpeakStart:   "00:30",
				OffpeakEnd:     "04:30",
				StandingCharge: 25.0,
			},
		},
	}

	if got := standingChargeForDay("2024-06-15", cfg); got != 25.0 {
		t.Errorf("standingChargeForDay = %v, want 25.0", got)
	}
	if got := standingChargeForDay("2023-06-15", cfg); got != 0 {
		t.Errorf("standingChargeForDay before any rate = %v, want 0", got)
	}
}

func TestParseHHMM(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"00:00", 0},
		{"00:30", 30},
		{"04:30", 270},
		{"12:00", 720},
		{"23:59", 1439},
	}
	for _, tt := range tests {
		got := parseHHMM(tt.input)
		if got != tt.want {
			t.Errorf("parseHHMM(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

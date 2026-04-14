package octopus

import (
	"testing"
	"time"
)

func TestIsAgile(t *testing.T) {
	tests := []struct {
		code string
		want bool
	}{
		{"E-1R-AGILE-24-10-01-E", true},
		{"E-1R-AGILE-FLEX-22-11-25-A", true},
		{"E-1R-GO-FIX-12-10-01-E", false},
		{"E-1R-VAR-22-11-01-A", false},
		{"", false},
	}
	for _, tt := range tests {
		a := &TariffAgreement{TariffCode: tt.code}
		if got := a.IsAgile(); got != tt.want {
			t.Errorf("IsAgile(%q) = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestAgreementAt(t *testing.T) {
	t1 := mustTime("2024-01-01T00:00:00Z")
	t2 := mustTime("2024-06-01T00:00:00Z")
	t3 := mustTime("2024-12-31T23:59:59Z")
	openEnd := mustTime("2030-01-01T00:00:00Z")

	agreements := []TariffAgreement{
		{TariffCode: "VAR-22-11-01", ValidFrom: t1, ValidTo: &t2},
		{TariffCode: "AGILE-24-10-01", ValidFrom: t2, ValidTo: nil},
	}

	tests := []struct {
		at   time.Time
		want string
	}{
		{mustTime("2024-03-15T12:00:00Z"), "VAR-22-11-01"},
		{mustTime("2024-06-01T00:00:00Z"), "AGILE-24-10-01"},
		{mustTime("2024-12-15T12:00:00Z"), "AGILE-24-10-01"},
		{mustTime("2023-12-31T23:59:59Z"), ""}, // before any agreement
	}

	for _, tt := range tests {
		a := AgreementAt(agreements, tt.at)
		if tt.want == "" {
			if a != nil {
				t.Errorf("AgreementAt(%v) = %v, want nil", tt.at, a.TariffCode)
			}
		} else {
			if a == nil {
				t.Errorf("AgreementAt(%v) = nil, want %s", tt.at, tt.want)
			} else if a.TariffCode != tt.want {
				t.Errorf("AgreementAt(%v) = %s, want %s", tt.at, a.TariffCode, tt.want)
			}
		}
	}

	_ = t3
	_ = openEnd
}

func TestAgreementAtEmpty(t *testing.T) {
	a := AgreementAt(nil, time.Now())
	if a != nil {
		t.Error("AgreementAt(nil, ...) should return nil")
	}

	a = AgreementAt([]TariffAgreement{}, time.Now())
	if a != nil {
		t.Error("AgreementAt([], ...) should return nil")
	}
}

func TestAgreementAtWithEnd(t *testing.T) {
	from := mustTime("2024-01-01T00:00:00Z")
	to := mustTime("2024-06-01T00:00:00Z")
	agreements := []TariffAgreement{
		{TariffCode: "AGO", ValidFrom: from, ValidTo: &to},
	}

	before := mustTime("2023-12-31T23:59:59Z")
	a := AgreementAt(agreements, before)
	if a != nil {
		t.Error("before start should be nil")
	}

	during := mustTime("2024-03-15T00:00:00Z")
	a = AgreementAt(agreements, during)
	if a == nil || a.TariffCode != "AGO" {
		t.Error("during range should match")
	}

	atEnd := mustTime("2024-06-01T00:00:00Z")
	a = AgreementAt(agreements, atEnd)
	if a != nil {
		t.Error("at exact end should be nil")
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

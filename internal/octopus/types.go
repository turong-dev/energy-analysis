package octopus

import (
	"strings"
	"time"
)

// HalfHourlyRate is a single half-hourly Agile rate slot.
type HalfHourlyRate struct {
	ValidFrom   time.Time `json:"valid_from"`
	ValidTo     time.Time `json:"valid_to"`
	ValueIncVAT float64   `json:"value_inc_vat"` // p/kWh
}

// HalfHourlyConsumption is a single half-hourly consumption reading from the SMETS2 meter.
type HalfHourlyConsumption struct {
	IntervalStart time.Time `json:"interval_start"`
	IntervalEnd   time.Time `json:"interval_end"`
	Consumption   float64   `json:"consumption"` // kWh
}

// TariffAgreement is a period during which a specific tariff was active on a meter point.
type TariffAgreement struct {
	TariffCode string     `json:"tariff_code"`
	ValidFrom  time.Time  `json:"valid_from"`
	ValidTo    *time.Time `json:"valid_to"` // nil = ongoing
}

// IsAgile reports whether the tariff code is an Agile tariff.
func (a *TariffAgreement) IsAgile() bool {
	return strings.Contains(strings.ToUpper(a.TariffCode), "AGILE")
}

// AgreementAt returns the agreement active at time t, or nil if none covers t.
func AgreementAt(agreements []TariffAgreement, t time.Time) *TariffAgreement {
	for i := range agreements {
		a := &agreements[i]
		if t.Before(a.ValidFrom) {
			continue
		}
		if a.ValidTo != nil && !t.Before(*a.ValidTo) {
			continue
		}
		return a
	}
	return nil
}

// MonthFile is what we store in S3 for a calendar month of rates or consumption.
type MonthFile[T any] struct {
	Month   string `json:"month"`   // YYYY-MM
	Updated string `json:"updated"` // RFC3339
	Data    []T    `json:"data"`
}

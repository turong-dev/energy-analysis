package octopus

import "time"

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

// MonthFile is what we store in S3 for a calendar month of rates or consumption.
type MonthFile[T any] struct {
	Month   string `json:"month"`   // YYYY-MM
	Updated string `json:"updated"` // RFC3339
	Data    []T    `json:"data"`
}

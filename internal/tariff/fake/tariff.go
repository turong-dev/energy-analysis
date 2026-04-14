package fake

import (
	"time"

	"energy-utility/internal/tariff"
)

type ImportTariff struct {
	NameVal           string
	CodeVal           string
	TariffTypeVal     tariff.TariffType
	RateVal           float64
	StandingChargeVal float64
}

func NewImportTariff(rate float64) *ImportTariff {
	return &ImportTariff{
		NameVal:           "Fake Import Tariff",
		CodeVal:           "FAKE-IMPORT",
		TariffTypeVal:     tariff.TariffFixed,
		RateVal:           rate,
		StandingChargeVal: 25.0,
	}
}

func (t *ImportTariff) Name() string            { return t.NameVal }
func (t *ImportTariff) Code() string            { return t.CodeVal }
func (t *ImportTariff) Type() tariff.TariffType { return t.TariffTypeVal }

func (t *ImportTariff) Rate(_ time.Time) (tariff.Rate, error) {
	return tariff.Rate{ValueIncVAT: t.RateVal}, nil
}

func (t *ImportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
	var rates []tariff.Rate
	for d := from; d.Before(to); d = d.Add(30 * time.Minute) {
		rates = append(rates, tariff.Rate{
			ValueIncVAT: t.RateVal,
			ValidFrom:   d,
			ValidTo:     d.Add(30 * time.Minute),
		})
	}
	return rates, nil
}

func (t *ImportTariff) StandingCharge(_ time.Time) (float64, error) {
	return t.StandingChargeVal, nil
}

type ExportTariff struct {
	NameVal       string
	CodeVal       string
	TariffTypeVal tariff.TariffType
	RateVal       float64
}

func NewExportTariff(rate float64) *ExportTariff {
	return &ExportTariff{
		NameVal:       "Fake Export Tariff",
		CodeVal:       "FAKE-OUTGOING",
		TariffTypeVal: tariff.TariffFixed,
		RateVal:       rate,
	}
}

func (t *ExportTariff) Name() string            { return t.NameVal }
func (t *ExportTariff) Code() string            { return t.CodeVal }
func (t *ExportTariff) Type() tariff.TariffType { return t.TariffTypeVal }

func (t *ExportTariff) Rate(_ time.Time) (tariff.Rate, error) {
	return tariff.Rate{ValueIncVAT: t.RateVal}, nil
}

func (t *ExportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
	var rates []tariff.Rate
	for d := from; d.Before(to); d = d.Add(30 * time.Minute) {
		rates = append(rates, tariff.Rate{
			ValueIncVAT: t.RateVal,
			ValidFrom:   d,
			ValidTo:     d.Add(30 * time.Minute),
		})
	}
	return rates, nil
}

// Package octopus provides MCP-compatible tariff implementations.
package octopus

import (
	"context"
	"fmt"
	"time"

	"energy-utility/internal/store"
	"energy-utility/internal/tariff"
)

// MCPImportTariff wraps Octopus import tariff data for the MCP server.
type MCPImportTariff struct {
	id             string
	name           string
	code           string
	tariffType     tariff.TariffType
	store          store.Store
	direction      string
	standingCharge float64
}

// NewMCPImportTariff creates a new MCP-compatible import tariff.
func NewMCPImportTariff(id, name, code string, t tariff.TariffType, s store.Store, sc float64) *MCPImportTariff {
	return &MCPImportTariff{
		id:             id,
		name:           name,
		code:           code,
		tariffType:     t,
		store:          s,
		direction:      "import",
		standingCharge: sc,
	}
}

func (t *MCPImportTariff) Name() string            { return t.name }
func (t *MCPImportTariff) Code() string            { return t.code }
func (t *MCPImportTariff) Type() tariff.TariffType { return t.tariffType }

func (t *MCPImportTariff) Rate(at time.Time) (tariff.Rate, error) {
	rates, err := t.Rates(at, at.Add(30*time.Minute))
	if err != nil {
		return tariff.Rate{}, err
	}
	if len(rates) == 0 {
		return tariff.Rate{}, fmt.Errorf("no rate found for %s", at)
	}
	return rates[0], nil
}

func (t *MCPImportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
	rates, err := ReadRates(context.Background(), t.store, t.direction, from, to)
	if err != nil {
		return nil, err
	}

	// Convert octopus.HalfHourlyRate to tariff.Rate
	result := make([]tariff.Rate, len(rates))
	for i, r := range rates {
		result[i] = tariff.Rate{
			ValueIncVAT: r.ValueIncVAT,
			ValidFrom:   r.ValidFrom,
			ValidTo:     r.ValidTo,
		}
	}
	return result, nil
}

func (t *MCPImportTariff) StandingCharge(_ time.Time) (float64, error) {
	return t.standingCharge, nil
}

// MCPExportTariff wraps Octopus export tariff data for the MCP server.
type MCPExportTariff struct {
	id         string
	name       string
	code       string
	tariffType tariff.TariffType
	store      store.Store
	direction  string
}

// NewMCPExportTariff creates a new MCP-compatible export tariff.
func NewMCPExportTariff(id, name, code string, t tariff.TariffType, s store.Store) *MCPExportTariff {
	return &MCPExportTariff{
		id:         id,
		name:       name,
		code:       code,
		tariffType: t,
		store:      s,
		direction:  "export",
	}
}

func (t *MCPExportTariff) Name() string            { return t.name }
func (t *MCPExportTariff) Code() string            { return t.code }
func (t *MCPExportTariff) Type() tariff.TariffType { return t.tariffType }

func (t *MCPExportTariff) Rate(at time.Time) (tariff.Rate, error) {
	rates, err := t.Rates(at, at.Add(30*time.Minute))
	if err != nil {
		return tariff.Rate{}, err
	}
	if len(rates) == 0 {
		return tariff.Rate{}, fmt.Errorf("no rate found for %s", at)
	}
	return rates[0], nil
}

func (t *MCPExportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
	rates, err := ReadRates(context.Background(), t.store, t.direction, from, to)
	if err != nil {
		return nil, err
	}

	// Convert octopus.HalfHourlyRate to tariff.Rate
	result := make([]tariff.Rate, len(rates))
	for i, r := range rates {
		result[i] = tariff.Rate{
			ValueIncVAT: r.ValueIncVAT,
			ValidFrom:   r.ValidFrom,
			ValidTo:     r.ValidTo,
		}
	}
	return result, nil
}

// Ensure interfaces are implemented.
var (
	_ tariff.ImportTariff = (*MCPImportTariff)(nil)
	_ tariff.ExportTariff = (*MCPExportTariff)(nil)
)

package octopus

import (
	"context"
	"time"

	"energy-utility/internal/tariff"
)

type ExportTariff struct {
	client      *Client
	productCode string
	region      string
	name        string
	code        string
	tariffType  tariff.TariffType
}

func NewExportTariff(client *Client, productCode, region string, tariffType tariff.TariffType) *ExportTariff {
	return &ExportTariff{
		client:      client,
		productCode: productCode,
		region:      region,
		name:        productName(productCode),
		code:        productCode,
		tariffType:  tariffType,
	}
}

func (t *ExportTariff) Name() string {
	return t.name
}

func (t *ExportTariff) Code() string {
	return t.code
}

func (t *ExportTariff) Type() tariff.TariffType {
	return t.tariffType
}

func (t *ExportTariff) Rate(t0 time.Time) (tariff.Rate, error) {
	code, rates, err := t.client.FetchRates(context.Background(), t.productCode, t.region, t0, t0.Add(48*time.Hour))
	if err != nil {
		return tariff.Rate{}, err
	}
	if len(rates) == 0 {
		return tariff.Rate{}, nil
	}
	t.code = code
	for _, r := range rates {
		if !t0.Before(r.ValidFrom) && t0.Before(r.ValidTo) {
			return tariff.Rate{
				ValueIncVAT: r.ValueIncVAT,
				ValidFrom:   r.ValidFrom,
				ValidTo:     r.ValidTo,
			}, nil
		}
	}
	return tariff.Rate{}, nil
}

func (t *ExportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
	code, rates, err := t.client.FetchRates(context.Background(), t.productCode, t.region, from, to)
	if err != nil {
		return nil, err
	}
	t.code = code
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

package octopus

import (
	"context"
	"fmt"
	"time"

	"energy-utility/internal/tariff"
)

type ImportTariff struct {
	client      *Client
	productCode string
	region      string
	name        string
	code        string
	tariffType  tariff.TariffType
}

func NewImportTariff(client *Client, productCode, region string, tariffType tariff.TariffType) *ImportTariff {
	return &ImportTariff{
		client:      client,
		productCode: productCode,
		region:      region,
		name:        productName(productCode),
		code:        productCode,
		tariffType:  tariffType,
	}
}

func (t *ImportTariff) Name() string {
	return t.name
}

func (t *ImportTariff) Code() string {
	return t.code
}

func (t *ImportTariff) Type() tariff.TariffType {
	return t.tariffType
}

func (t *ImportTariff) Rate(t0 time.Time) (tariff.Rate, error) {
	code, rates, err := t.client.FetchRates(context.Background(), t.productCode, t.region, t0, t0.Add(48*time.Hour))
	if err != nil {
		return tariff.Rate{}, err
	}
	if len(rates) == 0 {
		return tariff.Rate{}, fmt.Errorf("no rates found for %s at %s", t.productCode, t0)
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
	return tariff.Rate{}, fmt.Errorf("no rate found for %s at %s", t.productCode, t0)
}

func (t *ImportTariff) Rates(from, to time.Time) ([]tariff.Rate, error) {
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

func (t *ImportTariff) StandingCharge(t0 time.Time) (float64, error) {
	if t.code == "" {
		_, err := t.Rate(t0)
		if err != nil {
			return 0, err
		}
	}
	return t.client.FetchStandingCharge(context.Background(), t.code)
}

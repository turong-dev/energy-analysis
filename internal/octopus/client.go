package octopus

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	oe "github.com/danopstech/octopusenergy"
)

const apiBase = "https://api.octopus.energy/v1"

type Client struct {
	oc     *oe.Client
	apiKey string
	http   *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		oc:     oe.NewClient(oe.NewConfig().WithApiKey(apiKey)),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// ---------------------------------------------------------------------------
// Product discovery
// ---------------------------------------------------------------------------

// FindAgileProductAt returns the product code for the Agile product that was
// active at the given time, in the given direction ("IMPORT" or "EXPORT").
// It lists all products without date filtering and matches by available_from/available_to.
func (c *Client) FindAgileProductAt(ctx context.Context, direction string, at time.Time, logger func(string, ...any)) (string, error) {
	direction = strings.ToUpper(direction)

	page, err := c.oc.Product.ListPagesWithContext(ctx, &oe.ProductsListOptions{
		IsVariable: oe.Bool(true),
	})
	if err != nil {
		return "", err
	}

	// Find the most-recently-started Agile product whose window covers `at`.
	best := ""
	var bestFrom time.Time
	for _, p := range page.Results {
		if !strings.Contains(p.Code, "AGILE") {
			continue
		}
		isExport := strings.Contains(p.Code, "OUTGOING")
		if direction == "IMPORT" && isExport {
			continue
		}
		if direction == "EXPORT" && !isExport {
			continue
		}

		// Must have started on or before `at`
		if p.AvailableFrom.After(at) {
			continue
		}
		// If it has ended, must have ended after `at`
		if availTo, ok := p.AvailableTo.(string); ok && availTo != "" {
			t, err := time.Parse(time.RFC3339, availTo)
			if err == nil && !t.After(at) {
				continue
			}
		}

		logger("  candidate Agile product: %s (%s) from %s", p.Code, p.FullName, p.AvailableFrom.Format("2006-01-02"))
		if best == "" || p.AvailableFrom.After(bestFrom) {
			best = p.Code
			bestFrom = p.AvailableFrom
		}
	}

	if best == "" {
		return "", fmt.Errorf("no active Agile %s product found for %s", strings.ToLower(direction), at.Format("2006-01-02"))
	}
	logger("  selected: %s", best)
	return best, nil
}

// GSPRegion returns the single-letter GSP region code for a given MPAN
// by querying the electricity meter point endpoint (e.g. "_H" → "H").
func (c *Client) GSPRegion(ctx context.Context, mpan string) (string, error) {
	out, err := c.oc.MeterPoint.GetWithContext(ctx, &oe.MeterPointGetOptions{MPAN: mpan})
	if err != nil {
		return "", fmt.Errorf("lookup GSP for MPAN %s: %w", mpan, err)
	}
	// API returns "_H" style — strip the leading underscore
	return strings.TrimPrefix(out.GSP, "_"), nil
}

// tariffCodeFor fetches the actual tariff code for a product/region from the
// product details API. We make the call directly rather than via the danopstech
// client, which has a type mismatch bug on the discount fields (int vs float).
func (c *Client) tariffCodeFor(ctx context.Context, productCode, region string, at time.Time) (string, error) {
	url := fmt.Sprintf("%s/products/%s/?tariffs_active_at=%s", apiBase, productCode, at.UTC().Format(time.RFC3339))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if c.apiKey != "" {
		req.SetBasicAuth(c.apiKey, "")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
	}

	// Minimal struct — only the fields we need. Discount fields use json.Number
	// to avoid the int/float mismatch in the danopstech library.
	var product struct {
		SingleRegisterElectricityTariffs map[string]struct {
			DirectDebitMonthly struct {
				Code string `json:"code"`
			} `json:"direct_debit_monthly"`
		} `json:"single_register_electricity_tariffs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&product); err != nil {
		return "", err
	}

	key := "_" + strings.ToUpper(region)
	tariff, ok := product.SingleRegisterElectricityTariffs[key]
	if !ok {
		keys := make([]string, 0, len(product.SingleRegisterElectricityTariffs))
		for k := range product.SingleRegisterElectricityTariffs {
			keys = append(keys, k)
		}
		return "", fmt.Errorf("no tariff for region %s in product %s (available: %v)", region, productCode, keys)
	}
	return tariff.DirectDebitMonthly.Code, nil
}

// ---------------------------------------------------------------------------
// Agile rates
// ---------------------------------------------------------------------------

// FetchRates returns all half-hourly rates for a product/region between from and to.
// It looks up the actual tariff code from the product details API.
func (c *Client) FetchRates(ctx context.Context, productCode, region string, from, to time.Time) (string, []HalfHourlyRate, error) {
	mid := from.AddDate(0, 0, 14)
	if mid.After(to) {
		mid = from
	}
	tariffCode, err := c.tariffCodeFor(ctx, productCode, region, mid)
	if err != nil {
		return "", nil, fmt.Errorf("get tariff code: %w", err)
	}

	page, err := c.oc.TariffCharge.GetPagesWithContext(ctx, &oe.TariffChargesGetOptions{
		ProductCode: productCode,
		TariffCode:  tariffCode,
		FuelType:    oe.FuelTypeElectricity,
		Rate:        oe.RateStandardUnit,
		PeriodFrom:  oe.Time(from),
		PeriodTo:    oe.Time(to),
	})
	if err != nil {
		return "", nil, err
	}

	rates := make([]HalfHourlyRate, 0, len(page.Results))
	for _, r := range page.Results {
		rates = append(rates, HalfHourlyRate{
			ValidFrom:   r.ValidFrom,
			ValidTo:     r.ValidTo,
			ValueIncVAT: r.ValueIncVat,
		})
	}
	return tariffCode, rates, nil
}

// ---------------------------------------------------------------------------
// Account agreements
// ---------------------------------------------------------------------------

// FetchAgreements returns the tariff agreement history for the import and export
// electricity meter points on the given Octopus account.
func (c *Client) FetchAgreements(ctx context.Context, accountID string) (importAgreements, exportAgreements []TariffAgreement, err error) {
	url := fmt.Sprintf("%s/accounts/%s/", apiBase, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.SetBasicAuth(c.apiKey, "")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, nil, fmt.Errorf("accounts API HTTP %d: %s", resp.StatusCode, body)
	}

	var account struct {
		Properties []struct {
			ElectricityMeterPoints []struct {
				IsExport   bool `json:"is_export"`
				Agreements []struct {
					TariffCode string     `json:"tariff_code"`
					ValidFrom  time.Time  `json:"valid_from"`
					ValidTo    *time.Time `json:"valid_to"`
				} `json:"agreements"`
			} `json:"electricity_meter_points"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return nil, nil, fmt.Errorf("decode account response: %w", err)
	}

	for _, prop := range account.Properties {
		for _, mp := range prop.ElectricityMeterPoints {
			agreements := make([]TariffAgreement, len(mp.Agreements))
			for i, a := range mp.Agreements {
				agreements[i] = TariffAgreement{
					TariffCode: a.TariffCode,
					ValidFrom:  a.ValidFrom,
					ValidTo:    a.ValidTo,
				}
			}
			if mp.IsExport {
				exportAgreements = append(exportAgreements, agreements...)
			} else {
				importAgreements = append(importAgreements, agreements...)
			}
		}
	}
	return importAgreements, exportAgreements, nil
}

// ---------------------------------------------------------------------------
// SMETS2 consumption
// ---------------------------------------------------------------------------

// FetchConsumption returns half-hourly consumption readings for a meter between from and to.
func (c *Client) FetchConsumption(ctx context.Context, mpan, serial string, from, to time.Time) ([]HalfHourlyConsumption, error) {
	page, err := c.oc.Consumption.GetPagesWithContext(ctx, &oe.ConsumptionGetOptions{
		MPN:          mpan,
		SerialNumber: serial,
		FuelType:     oe.FuelTypeElectricity,
		PeriodFrom:   oe.Time(from),
		PeriodTo:     oe.Time(to),
		OrderBy:      oe.String("period"),
	})
	if err != nil {
		return nil, err
	}

	readings := make([]HalfHourlyConsumption, 0, len(page.Results))
	for _, r := range page.Results {
		start, _ := time.Parse(time.RFC3339, r.IntervalStart)
		end, _ := time.Parse(time.RFC3339, r.IntervalEnd)
		readings = append(readings, HalfHourlyConsumption{
			IntervalStart: start,
			IntervalEnd:   end,
			Consumption:   r.Consumption,
		})
	}
	return readings, nil
}

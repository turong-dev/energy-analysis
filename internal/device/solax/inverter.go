package solax

import (
	"context"
	"math"
	"time"

	"energy-utility/internal/device"
)

const fiveMinutes = 5 * time.Minute

type Inverter struct {
	client *Client
}

func NewInverter(client *Client) *Inverter {
	return &Inverter{client: client}
}

func (i *Inverter) Manufacturer() string {
	return "SolaX"
}

func (i *Inverter) Model() string {
	return "Unknown"
}

func (i *Inverter) DataResolution() time.Duration {
	return fiveMinutes
}

func (i *Inverter) FetchDay(ctx context.Context, siteID string, day time.Time) (*device.DayData, error) {
	raw, err := i.client.GetEnergyInfo(siteID, day)
	if err != nil {
		return nil, err
	}
	if raw == nil || !i.client.HasRealData(raw) {
		return nil, nil
	}

	return parseRaw(raw, day), nil
}

func parseRaw(raw *Raw, date time.Time) *device.DayData {
	toTimeSeries := func(pts []DataPoint) device.TimeSeries {
		values := make([]float64, len(pts))
		for i, p := range pts {
			if p.Yais != nil {
				values[i] = *p.Yais
			} else {
				values[i] = math.NaN()
			}
		}
		return device.TimeSeries{
			Resolution: fiveMinutes,
			Values:     values,
		}
	}

	return &device.DayData{
		Date:             date,
		Resolution:       fiveMinutes,
		TotalYield:       raw.Yield.TotalYield,
		FeedIn:           raw.FeedInEnergy,
		GridImport:       raw.Consumed.Grid,
		BatteryCharge:    raw.ChargeYield,
		BatteryDischarge: raw.DisChargeYield,
		Load:             raw.ConsumeEnergy,
		PVPower:          toTimeSeries(raw.Records.PVPower),
		LoadPower:        toTimeSeries(raw.Records.LoadPower),
		BatteryPower:     toTimeSeries(raw.Records.BatteryPower),
		BatterySoC:       toTimeSeries(raw.Records.BatterySoC),
	}
}

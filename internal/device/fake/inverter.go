package fake

import (
	"context"
	"time"

	"energy-utility/internal/device"
)

type Inverter struct {
	ManufacturerVal string
	ModelVal        string
	ResolutionVal   time.Duration
	Data            []device.DayData
}

func NewInverter() *Inverter {
	return &Inverter{
		ManufacturerVal: "Fake",
		ModelVal:        "Test Model",
		ResolutionVal:   5 * time.Minute,
		Data:            []device.DayData{},
	}
}

func (i *Inverter) Manufacturer() string          { return i.ManufacturerVal }
func (i *Inverter) Model() string                 { return i.ModelVal }
func (i *Inverter) DataResolution() time.Duration { return i.ResolutionVal }

func (i *Inverter) FetchDay(_ context.Context, _ string, day time.Time) (*device.DayData, error) {
	for _, d := range i.Data {
		if d.Date.Format("2006-01-02") == day.Format("2006-01-02") {
			return &d, nil
		}
	}
	return nil, nil
}

func (i *Inverter) AddDay(d device.DayData) {
	d.Resolution = i.ResolutionVal
	i.Data = append(i.Data, d)
}

func MakeDayData(date string, totalYield float64, socValues []float64) device.DayData {
	d, _ := time.Parse("2006-01-02", date)
	soc := device.TimeSeries{
		Resolution: 5 * time.Minute,
		Values:     socValues,
	}
	return device.DayData{
		Date:       d,
		Resolution: 5 * time.Minute,
		TotalYield: totalYield,
		BatterySoC: soc,
	}
}

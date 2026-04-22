// Package solax provides MCP-compatible device implementations.
package solax

import (
	"context"
	"fmt"
	"time"

	"energy-utility/internal/device"
	"energy-utility/internal/store"
)

// MCPDevice wraps SolaX data access for the MCP server.
// It implements device.EnergyDevice using S3 storage.
type MCPDevice struct {
	siteID string
	store  store.Store
}

// NewMCPDevice creates a new MCP-compatible SolaX device.
func NewMCPDevice(siteID string, s store.Store) *MCPDevice {
	return &MCPDevice{
		siteID: siteID,
		store:  s,
	}
}

func (d *MCPDevice) Manufacturer() string          { return "SolaX" }
func (d *MCPDevice) Model() string                 { return "X3-Hybrid" }
func (d *MCPDevice) DataResolution() time.Duration { return 5 * time.Minute }

func (d *MCPDevice) FetchDay(ctx context.Context, _ string, day time.Time) (*device.DayData, error) {
	dateStr := day.Format("2006-01-02")

	// Fetch from S3 using the standard key format
	key := fmt.Sprintf("solax/raw/%s/%s/%s/daily-detail.json",
		day.Format("2006"), day.Format("01"), day.Format("02"))

	var raw Raw
	if err := d.store.GetJSON(ctx, key, &raw); err != nil {
		if store.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fetch %s: %w", key, err)
	}

	record := ParseRaw(&raw, dateStr)
	data := dayRecordToDeviceData(record)

	return &data, nil
}

// dayRecordToDeviceData converts a DayRecord to device.DayData.
func dayRecordToDeviceData(r DayRecord) device.DayData {
	date, _ := time.Parse("2006-01-02", r.Date)
	return device.DayData{
		Date:             date,
		Resolution:       5 * time.Minute,
		TotalYield:       r.TotalYield,
		FeedIn:           r.FeedIn,
		GridImport:       r.GridImport,
		BatteryCharge:    r.BatteryCharge,
		BatteryDischarge: r.BatteryDischarge,
		Load:             r.TotalLoad,
		PVPower: device.TimeSeries{
			Resolution: 5 * time.Minute,
			Values:     r.PVPower,
		},
		LoadPower: device.TimeSeries{
			Resolution: 5 * time.Minute,
			Values:     r.LoadPower,
		},
		BatteryPower: device.TimeSeries{
			Resolution: 5 * time.Minute,
			Values:     r.BatteryPower,
		},
		BatterySoC: device.TimeSeries{
			Resolution: 5 * time.Minute,
			Values:     r.BatterySoC,
		},
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"energy-utility/internal/device"
	"energy-utility/internal/tariff"
)

// readDeviceResource handles device-specific resource URIs.
func (s *Server) readDeviceResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	// URI format: energy://site/{siteID}/device/{deviceID}/day/{date}
	// or: energy://site/{siteID}/device/{deviceID}/range/{from}/{to}
	// or: energy://site/{siteID}/device/{deviceID}/meta
	// or: energy://site/{siteID}/devices

	parts := strings.Split(strings.TrimPrefix(uri, "energy://"), "/")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid device URI: %s", uri)
	}

	// parts[0] = "site", parts[1] = siteID, parts[2] = "device", parts[3] = deviceID
	if parts[0] != "site" || parts[2] != "device" {
		return nil, fmt.Errorf("invalid device URI format: %s", uri)
	}

	siteID := parts[1]
	deviceID := parts[3]

	// Check for devices list request
	if deviceID == "devices" && len(parts) == 4 {
		return s.readDevicesListResource(siteID)
	}

	if len(parts) < 5 {
		return nil, fmt.Errorf("invalid device URI, missing action: %s", uri)
	}

	action := parts[4]

	d, ok := s.GetDevice(siteID, deviceID)
	if !ok {
		return nil, fmt.Errorf("device not found: %s/%s", siteID, deviceID)
	}

	switch action {
	case "day":
		if len(parts) != 6 {
			return nil, fmt.Errorf("invalid day URI, expected date: %s", uri)
		}
		date, err := parseDate(parts[5])
		if err != nil {
			return nil, fmt.Errorf("invalid date: %v", err)
		}
		return s.readDayResource(ctx, uri, d, date)

	case "range":
		if len(parts) != 7 {
			return nil, fmt.Errorf("invalid range URI, expected from/to: %s", uri)
		}
		from, err := parseDate(parts[5])
		if err != nil {
			return nil, fmt.Errorf("invalid from date: %v", err)
		}
		to, err := parseDate(parts[6])
		if err != nil {
			return nil, fmt.Errorf("invalid to date: %v", err)
		}
		return s.readRangeResource(ctx, uri, siteID, d, from, to)

	case "meta":
		return s.readDeviceMetaResource(uri, d)

	default:
		return nil, fmt.Errorf("unknown device action: %s", action)
	}
}

func (s *Server) readDevicesListResource(siteID string) ([]ResourceContents, error) {
	devices := s.ListDevices(siteID)
	data, err := json.Marshal(devices)
	if err != nil {
		return nil, err
	}

	return []ResourceContents{{
		URI:      fmt.Sprintf("energy://site/%s/devices", siteID),
		MimeType: "application/json",
		Text:     string(data),
	}}, nil
}

func (s *Server) readDayResource(ctx context.Context, uri string, d device.EnergyDevice, date time.Time) ([]ResourceContents, error) {
	// Use empty siteID since device is already resolved
	data, err := d.FetchDay(ctx, "", date)
	if err != nil {
		return nil, fmt.Errorf("fetch day: %w", err)
	}

	if data == nil {
		// Return empty content for missing data
		return []ResourceContents{{
			URI:      uri,
			MimeType: "application/json",
			Text:     "null",
		}}, nil
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("marshal day data: %w", err)
	}

	return []ResourceContents{{
		URI:      uri,
		MimeType: "application/json",
		Text:     string(jsonData),
	}}, nil
}

func (s *Server) readRangeResource(ctx context.Context, uri string, siteID string, d device.EnergyDevice, from, to time.Time) ([]ResourceContents, error) {
	var days []device.DayData

	// Iterate through each day in the range
	for day := from; !day.After(to); day = day.AddDate(0, 0, 1) {
		data, err := d.FetchDay(ctx, "", day)
		if err != nil {
			logError("fetch day %s: %v", day.Format("2006-01-02"), err)
			continue
		}
		if data != nil {
			days = append(days, *data)
		}
	}

	jsonData, err := json.Marshal(days)
	if err != nil {
		return nil, fmt.Errorf("marshal range data: %w", err)
	}

	return []ResourceContents{{
		URI:      uri,
		MimeType: "application/json",
		Text:     string(jsonData),
	}}, nil
}

func (s *Server) readDeviceMetaResource(uri string, d device.EnergyDevice) ([]ResourceContents, error) {
	meta := struct {
		Manufacturer     string `json:"manufacturer"`
		Model            string `json:"model"`
		DataResolution   int64  `json:"data_resolution_ms"`
		DataResolutionHR string `json:"data_resolution"`
	}{
		Manufacturer:     d.Manufacturer(),
		Model:            d.Model(),
		DataResolution:   d.DataResolution().Milliseconds(),
		DataResolutionHR: d.DataResolution().String(),
	}

	jsonData, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal meta: %w", err)
	}

	return []ResourceContents{{
		URI:      uri,
		MimeType: "application/json",
		Text:     string(jsonData),
	}}, nil
}

// readTariffResource handles tariff-specific resource URIs.
func (s *Server) readTariffResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	// URI format: energy://tariff/{direction}/{tariffID}/rates/{from}/{to}
	// or: energy://tariff/{direction}/{tariffID}/agreement
	// or: energy://tariffs/{direction}

	parts := strings.Split(strings.TrimPrefix(uri, "energy://"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid tariff URI: %s", uri)
	}

	// Check for tariffs list
	if parts[0] == "tariffs" {
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid tariffs list URI: %s", uri)
		}
		direction := parts[1]
		return s.readTariffsListResource(direction)
	}

	if parts[0] != "tariff" || len(parts) < 4 {
		return nil, fmt.Errorf("invalid tariff URI format: %s", uri)
	}

	direction := parts[1]
	tariffID := parts[2]
	action := parts[3]

	switch action {
	case "rates":
		if len(parts) != 6 {
			return nil, fmt.Errorf("invalid rates URI, expected from/to dates: %s", uri)
		}
		from, err := parseDate(parts[4])
		if err != nil {
			return nil, fmt.Errorf("invalid from date: %v", err)
		}
		to, err := parseDate(parts[5])
		if err != nil {
			return nil, fmt.Errorf("invalid to date: %v", err)
		}
		return s.readRatesResource(ctx, uri, direction, tariffID, from, to)

	case "agreement":
		return s.readAgreementResource(uri, direction, tariffID)

	default:
		return nil, fmt.Errorf("unknown tariff action: %s", action)
	}
}

func (s *Server) readTariffsListResource(direction string) ([]ResourceContents, error) {
	var ids []string
	switch direction {
	case "import":
		ids = s.ListImportTariffs()
	case "export":
		ids = s.ListExportTariffs()
	default:
		return nil, fmt.Errorf("invalid direction: %s", direction)
	}

	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}

	return []ResourceContents{{
		URI:      fmt.Sprintf("energy://tariffs/%s", direction),
		MimeType: "application/json",
		Text:     string(data),
	}}, nil
}

func (s *Server) readRatesResource(ctx context.Context, uri, direction, tariffID string, from, to time.Time) ([]ResourceContents, error) {
	var rates []tariff.Rate
	var err error

	switch direction {
	case "import":
		t, ok := s.GetImportTariff(tariffID)
		if !ok {
			return nil, fmt.Errorf("import tariff not found: %s", tariffID)
		}
		rates, err = t.Rates(from, to)

	case "export":
		t, ok := s.GetExportTariff(tariffID)
		if !ok {
			return nil, fmt.Errorf("export tariff not found: %s", tariffID)
		}
		rates, err = t.Rates(from, to)

	default:
		return nil, fmt.Errorf("invalid direction: %s", direction)
	}

	if err != nil {
		return nil, fmt.Errorf("fetch rates: %w", err)
	}

	jsonData, err := json.Marshal(rates)
	if err != nil {
		return nil, fmt.Errorf("marshal rates: %w", err)
	}

	return []ResourceContents{{
		URI:      uri,
		MimeType: "application/json",
		Text:     string(jsonData),
	}}, nil
}

func (s *Server) readAgreementResource(uri, direction, tariffID string) ([]ResourceContents, error) {
	var agreement struct {
		ID        string `json:"id"`
		Direction string `json:"direction"`
		Name      string `json:"name"`
		Code      string `json:"code"`
		Type      string `json:"type"`
	}

	agreement.ID = tariffID
	agreement.Direction = direction

	switch direction {
	case "import":
		t, ok := s.GetImportTariff(tariffID)
		if !ok {
			return nil, fmt.Errorf("import tariff not found: %s", tariffID)
		}
		agreement.Name = t.Name()
		agreement.Code = t.Code()
		agreement.Type = tariffTypeString(t.Type())

	case "export":
		t, ok := s.GetExportTariff(tariffID)
		if !ok {
			return nil, fmt.Errorf("export tariff not found: %s", tariffID)
		}
		agreement.Name = t.Name()
		agreement.Code = t.Code()
		agreement.Type = tariffTypeString(t.Type())

	default:
		return nil, fmt.Errorf("invalid direction: %s", direction)
	}

	jsonData, err := json.Marshal(agreement)
	if err != nil {
		return nil, fmt.Errorf("marshal agreement: %w", err)
	}

	return []ResourceContents{{
		URI:      uri,
		MimeType: "application/json",
		Text:     string(jsonData),
	}}, nil
}

func tariffTypeString(t tariff.TariffType) string {
	switch t {
	case tariff.TariffFixed:
		return "fixed"
	case tariff.TariffTimeOfUse:
		return "time_of_use"
	case tariff.TariffDynamic:
		return "dynamic"
	default:
		return "unknown"
	}
}

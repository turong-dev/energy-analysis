package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"energy-utility/internal/device"
	"energy-utility/internal/jsonutil"
)

// buildToolList returns all available MCP tools.
func (s *Server) buildToolList() []Tool {
	return []Tool{
		{
			Name:        "get_device_day",
			Description: "Retrieve energy device data for a specific day",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"siteID": map[string]interface{}{
						"type":        "string",
						"description": "Site identifier",
					},
					"deviceID": map[string]interface{}{
						"type":        "string",
						"description": "Device identifier",
					},
					"date": map[string]interface{}{
						"type":        "string",
						"description": "Date in YYYY-MM-DD format",
					},
				},
				Required: []string{"siteID", "deviceID", "date"},
			},
		},
		{
			Name:        "get_device_range",
			Description: "Retrieve energy device data for a date range",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"siteID": map[string]interface{}{
						"type":        "string",
						"description": "Site identifier",
					},
					"deviceID": map[string]interface{}{
						"type":        "string",
						"description": "Device identifier",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Start date in YYYY-MM-DD format",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "End date in YYYY-MM-DD format",
					},
				},
				Required: []string{"siteID", "deviceID", "from", "to"},
			},
		},
		{
			Name:        "get_import_rates",
			Description: "Retrieve import tariff rates for a date range",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tariffID": map[string]interface{}{
						"type":        "string",
						"description": "Import tariff identifier",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Start date in YYYY-MM-DD format",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "End date in YYYY-MM-DD format",
					},
				},
				Required: []string{"tariffID", "from", "to"},
			},
		},
		{
			Name:        "get_export_rates",
			Description: "Retrieve export tariff rates for a date range",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"tariffID": map[string]interface{}{
						"type":        "string",
						"description": "Export tariff identifier",
					},
					"from": map[string]interface{}{
						"type":        "string",
						"description": "Start date in YYYY-MM-DD format",
					},
					"to": map[string]interface{}{
						"type":        "string",
						"description": "End date in YYYY-MM-DD format",
					},
				},
				Required: []string{"tariffID", "from", "to"},
			},
		},
		{
			Name:        "list_sites",
			Description: "List all available site IDs",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
			},
		},
		{
			Name:        "list_devices",
			Description: "List all devices at a site",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"siteID": map[string]interface{}{
						"type":        "string",
						"description": "Site identifier",
					},
				},
				Required: []string{"siteID"},
			},
		},
		{
			Name:        "list_import_tariffs",
			Description: "List all available import tariff IDs",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
			},
		},
		{
			Name:        "list_export_tariffs",
			Description: "List all available export tariff IDs",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]interface{}{},
			},
		},
	}
}

// callTool executes an MCP tool.
func (s *Server) callTool(ctx context.Context, name string, args json.RawMessage) (*CallToolResult, error) {
	switch name {
	case "get_device_day":
		return s.callGetDeviceDay(ctx, args)
	case "get_device_range":
		return s.callGetDeviceRange(ctx, args)
	case "get_import_rates":
		return s.callGetImportRates(ctx, args)
	case "get_export_rates":
		return s.callGetExportRates(ctx, args)
	case "list_sites":
		return s.callListSites()
	case "list_devices":
		return s.callListDevices(args)
	case "list_import_tariffs":
		return s.callListImportTariffs()
	case "list_export_tariffs":
		return s.callListExportTariffs()
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// Tool argument structs.
type getDeviceDayArgs struct {
	SiteID   string `json:"siteID"`
	DeviceID string `json:"deviceID"`
	Date     string `json:"date"`
}

type getDeviceRangeArgs struct {
	SiteID   string `json:"siteID"`
	DeviceID string `json:"deviceID"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type getRatesArgs struct {
	TariffID string `json:"tariffID"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type listDevicesArgs struct {
	SiteID string `json:"siteID"`
}

func (s *Server) callGetDeviceDay(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var params getDeviceDayArgs
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	date, err := parseDate(params.Date)
	if err != nil {
		return nil, fmt.Errorf("invalid date: %w", err)
	}

	d, ok := s.GetDevice(params.SiteID, params.DeviceID)
	if !ok {
		return nil, fmt.Errorf("device not found: %s/%s", params.SiteID, params.DeviceID)
	}

	data, err := d.FetchDay(ctx, "", date)
	if err != nil {
		return nil, fmt.Errorf("fetch day: %w", err)
	}

	if data == nil {
		return &CallToolResult{
			Content: []ToolContent{{
				Type: "text",
				Text: "null",
			}},
		}, nil
	}

	jsonData, err := jsonutil.MarshalNoNaN(data)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callGetDeviceRange(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var params getDeviceRangeArgs
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	from, err := parseDate(params.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date: %w", err)
	}

	to, err := parseDate(params.To)
	if err != nil {
		return nil, fmt.Errorf("invalid to date: %w", err)
	}

	d, ok := s.GetDevice(params.SiteID, params.DeviceID)
	if !ok {
		return nil, fmt.Errorf("device not found: %s/%s", params.SiteID, params.DeviceID)
	}

	var days []device.DayData
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

	jsonData, err := jsonutil.MarshalNoNaN(days)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callGetImportRates(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var params getRatesArgs
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	from, err := parseDate(params.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date: %w", err)
	}

	to, err := parseDate(params.To)
	if err != nil {
		return nil, fmt.Errorf("invalid to date: %w", err)
	}

	t, ok := s.GetImportTariff(params.TariffID)
	if !ok {
		return nil, fmt.Errorf("import tariff not found: %s", params.TariffID)
	}

	rates, err := t.Rates(from, to)
	if err != nil {
		return nil, fmt.Errorf("fetch rates: %w", err)
	}

	jsonData, err := json.Marshal(rates)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callGetExportRates(ctx context.Context, args json.RawMessage) (*CallToolResult, error) {
	var params getRatesArgs
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	from, err := parseDate(params.From)
	if err != nil {
		return nil, fmt.Errorf("invalid from date: %w", err)
	}

	to, err := parseDate(params.To)
	if err != nil {
		return nil, fmt.Errorf("invalid to date: %w", err)
	}

	t, ok := s.GetExportTariff(params.TariffID)
	if !ok {
		return nil, fmt.Errorf("export tariff not found: %s", params.TariffID)
	}

	rates, err := t.Rates(from, to)
	if err != nil {
		return nil, fmt.Errorf("fetch rates: %w", err)
	}

	jsonData, err := json.Marshal(rates)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callListSites() (*CallToolResult, error) {
	sites := s.ListSites()
	jsonData, err := json.Marshal(sites)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callListDevices(args json.RawMessage) (*CallToolResult, error) {
	var params listDevicesArgs
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}

	devices := s.ListDevices(params.SiteID)
	jsonData, err := json.Marshal(devices)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callListImportTariffs() (*CallToolResult, error) {
	tariffs := s.ListImportTariffs()
	jsonData, err := json.Marshal(tariffs)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

func (s *Server) callListExportTariffs() (*CallToolResult, error) {
	tariffs := s.ListExportTariffs()
	jsonData, err := json.Marshal(tariffs)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	return &CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: string(jsonData),
		}},
	}, nil
}

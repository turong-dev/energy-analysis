package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"energy-utility/internal/device"
	"energy-utility/internal/device/fake"
	tarifffake "energy-utility/internal/tariff/fake"
)

func TestServerInitialization(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Test initialize
	resp, err := transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
		ClientInfo: Implementation{
			Name:    "test-client",
			Version: "1.0.0",
		},
	})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("initialize returned error: %s", resp.Error.Message)
	}

	var result InitializeResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %s, want %s", result.ProtocolVersion, ProtocolVersion)
	}

	if result.ServerInfo.Name != ServerName {
		t.Errorf("server name = %s, want %s", result.ServerInfo.Name, ServerName)
	}
}

func TestResourcesList(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register a device
	inv := fake.NewInverter()
	server.RegisterDevice("home", "inverter-1", inv)

	// Register tariffs
	importT := tarifffake.NewImportTariff(25.0)
	exportT := tarifffake.NewExportTariff(15.0)
	server.RegisterImportTariff("octopus-agile", importT)
	server.RegisterExportTariff("octopus-outgoing", exportT)

	// Initialize first
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// List resources
	resp, err := transport.Send("resources/list", nil)
	if err != nil {
		t.Fatalf("resources/list failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("resources/list returned error: %s", resp.Error.Message)
	}

	var result ListResourcesResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Should have resources for device, tariffs, and discovery
	if len(result.Resources) == 0 {
		t.Error("expected resources, got none")
	}

	// Check for specific resources
	var hasDayResource, hasImportRates, hasSites bool
	for _, r := range result.Resources {
		if r.URI == "energy://site/home/device/inverter-1/day/{YYYY-MM-DD}" {
			hasDayResource = true
		}
		if r.URI == "energy://tariff/import/octopus-agile/rates/{from}/{to}" {
			hasImportRates = true
		}
		if r.URI == "energy://sites" {
			hasSites = true
		}
	}

	if !hasDayResource {
		t.Error("missing device day resource")
	}
	if !hasImportRates {
		t.Error("missing import rates resource")
	}
	if !hasSites {
		t.Error("missing sites resource")
	}
}

func TestToolsList(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// List tools
	resp, err := transport.Send("tools/list", nil)
	if err != nil {
		t.Fatalf("tools/list failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("tools/list returned error: %s", resp.Error.Message)
	}

	var result ListToolsResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Should have tools
	if len(result.Tools) == 0 {
		t.Error("expected tools, got none")
	}

	// Check for specific tools
	expectedTools := map[string]bool{
		"get_device_day":      false,
		"get_import_rates":    false,
		"list_sites":          false,
		"list_devices":        false,
		"list_import_tariffs": false,
	}

	for _, tool := range result.Tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestGetDeviceDayTool(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register a device with data
	inv := fake.NewInverter()
	dayData := fake.MakeDayData("2025-04-18", 15.5, []float64{50.0, 55.0, 60.0})
	inv.AddDay(dayData)
	server.RegisterDevice("home", "inverter-1", inv)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Call tool
	resp, err := transport.Send("tools/call", CallToolParams{
		Name: "get_device_day",
		Arguments: mustJSON(map[string]string{
			"siteID":   "home",
			"deviceID": "inverter-1",
			"date":     "2025-04-18",
		}),
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("tool call returned error: %s", resp.Error.Message)
	}

	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if result.IsError {
		t.Error("tool returned error flag")
	}

	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}

	// Parse the day data from content
	var day device.DayData
	if err := json.Unmarshal([]byte(result.Content[0].Text), &day); err != nil {
		t.Fatalf("unmarshal day data: %v", err)
	}

	if day.TotalYield != 15.5 {
		t.Errorf("total yield = %f, want 15.5", day.TotalYield)
	}
}

func TestGetDeviceDayNotFound(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register a device without data
	inv := fake.NewInverter()
	server.RegisterDevice("home", "inverter-1", inv)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Call tool for non-existent day
	resp, err := transport.Send("tools/call", CallToolParams{
		Name: "get_device_day",
		Arguments: mustJSON(map[string]string{
			"siteID":   "home",
			"deviceID": "inverter-1",
			"date":     "2025-04-18",
		}),
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("tool call returned error: %s", resp.Error.Message)
	}

	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Should return "null" for missing data
	if result.Content[0].Text != "null" {
		t.Errorf("expected null for missing data, got: %s", result.Content[0].Text)
	}
}

func TestListSitesTool(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register devices at different sites
	inv1 := fake.NewInverter()
	inv2 := fake.NewInverter()
	server.RegisterDevice("home", "inverter-1", inv1)
	server.RegisterDevice("garage", "inverter-2", inv2)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Call tool
	resp, err := transport.Send("tools/call", CallToolParams{
		Name:      "list_sites",
		Arguments: mustJSON(map[string]interface{}{}),
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var sites []string
	if err := json.Unmarshal([]byte(result.Content[0].Text), &sites); err != nil {
		t.Fatalf("unmarshal sites: %v", err)
	}

	if len(sites) != 2 {
		t.Errorf("expected 2 sites, got %d: %v", len(sites), sites)
	}
}

func TestGetRatesTool(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register tariffs
	importT := tarifffake.NewImportTariff(25.0)
	exportT := tarifffake.NewExportTariff(15.0)
	server.RegisterImportTariff("octopus-agile", importT)
	server.RegisterExportTariff("octopus-outgoing", exportT)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Test import rates
	from := time.Date(2025, 4, 18, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	resp, err := transport.Send("tools/call", CallToolParams{
		Name: "get_import_rates",
		Arguments: mustJSON(map[string]string{
			"tariffID": "octopus-agile",
			"from":     from.Format("2006-01-02"),
			"to":       to.Format("2006-01-02"),
		}),
	})
	if err != nil {
		t.Fatalf("tool call failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("tool call returned error: %s", resp.Error.Message)
	}

	var result CallToolResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Parse rates (tariff.Rate has no JSON tags, so field names match struct)
	var rates []struct {
		ValueIncVAT float64   `json:"ValueIncVAT"`
		ValidFrom   time.Time `json:"ValidFrom"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &rates); err != nil {
		t.Fatalf("unmarshal rates: %v", err)
	}

	// Should have 48 half-hour slots for a day
	if len(rates) != 48 {
		t.Errorf("expected 48 rate slots, got %d", len(rates))
	}

	// All rates should be 25.0 (from fake tariff)
	for _, r := range rates {
		if r.ValueIncVAT != 25.0 {
			t.Errorf("rate = %f, want 25.0", r.ValueIncVAT)
			break
		}
	}
}

func TestReadSitesResource(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register devices
	inv := fake.NewInverter()
	server.RegisterDevice("home", "inverter-1", inv)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Read resource
	resp, err := transport.Send("resources/read", ReadResourceParams{
		URI: "energy://sites",
	})
	if err != nil {
		t.Fatalf("resources/read failed: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("resources/read returned error: %s", resp.Error.Message)
	}

	var result ReadResourceResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	if len(result.Contents) == 0 {
		t.Fatal("no content in result")
	}

	var sites []string
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &sites); err != nil {
		t.Fatalf("unmarshal sites: %v", err)
	}

	if len(sites) != 1 || sites[0] != "home" {
		t.Errorf("sites = %v, want [home]", sites)
	}
}

func TestReadDeviceResource(t *testing.T) {
	server := NewServer()
	transport := NewInMemoryTransport(server)
	defer transport.Close()

	// Register device with data
	inv := fake.NewInverter()
	dayData := fake.MakeDayData("2025-04-18", 20.0, []float64{45.0, 50.0})
	inv.AddDay(dayData)
	server.RegisterDevice("home", "inverter-1", inv)

	// Initialize
	transport.Send("initialize", InitializeParams{
		ProtocolVersion: ProtocolVersion,
	})

	// Read day resource
	resp, err := transport.Send("resources/read", ReadResourceParams{
		URI: "energy://site/home/device/inverter-1/day/2025-04-18",
	})
	if err != nil {
		t.Fatalf("resources/read failed: %v", err)
	}

	var result ReadResourceResult
	data, _ := json.Marshal(resp.Result)
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var day device.DayData
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &day); err != nil {
		t.Fatalf("unmarshal day: %v", err)
	}

	if day.TotalYield != 20.0 {
		t.Errorf("total yield = %f, want 20.0", day.TotalYield)
	}
}

// Helper function to convert map to JSON bytes.
func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"energy-utility/internal/device"
	"energy-utility/internal/tariff"
)

const (
	ProtocolVersion = "2024-11-05"
	ServerName      = "energy-utility-mcp"
	ServerVersion   = "1.0.0"
)

// Server implements the MCP protocol for energy data access.
type Server struct {
	mu sync.RWMutex

	// Device registries: siteID -> deviceID -> device
	devices map[string]map[string]device.EnergyDevice

	// Tariff registries
	importTariffs map[string]tariff.ImportTariff
	exportTariffs map[string]tariff.ExportTariff

	// Cache for date listings
	dateCache *DateCache

	// SSE transport
	sessions  map[string]*Session
	sessionMu sync.RWMutex
}

// Session represents an active MCP client connection.
type Session struct {
	ID      string
	WriteCh chan []byte
	Ctx     context.Context
	Cancel  context.CancelFunc
}

// NewServer creates a new MCP server.
func NewServer() *Server {
	return &Server{
		devices:       make(map[string]map[string]device.EnergyDevice),
		importTariffs: make(map[string]tariff.ImportTariff),
		exportTariffs: make(map[string]tariff.ExportTariff),
		dateCache:     NewDateCache(),
		sessions:      make(map[string]*Session),
	}
}

// RegisterDevice adds a device to the server.
func (s *Server) RegisterDevice(siteID, deviceID string, d device.EnergyDevice) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.devices[siteID] == nil {
		s.devices[siteID] = make(map[string]device.EnergyDevice)
	}
	s.devices[siteID][deviceID] = d
}

// RegisterImportTariff adds an import tariff to the server.
func (s *Server) RegisterImportTariff(tariffID string, t tariff.ImportTariff) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.importTariffs[tariffID] = t
}

// RegisterExportTariff adds an export tariff to the server.
func (s *Server) RegisterExportTariff(tariffID string, t tariff.ExportTariff) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exportTariffs[tariffID] = t
}

// GetDevice retrieves a registered device.
func (s *Server) GetDevice(siteID, deviceID string) (device.EnergyDevice, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	site, ok := s.devices[siteID]
	if !ok {
		return nil, false
	}
	d, ok := site[deviceID]
	return d, ok
}

// GetImportTariff retrieves a registered import tariff.
func (s *Server) GetImportTariff(tariffID string) (tariff.ImportTariff, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.importTariffs[tariffID]
	return t, ok
}

// GetExportTariff retrieves a registered export tariff.
func (s *Server) GetExportTariff(tariffID string) (tariff.ExportTariff, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.exportTariffs[tariffID]
	return t, ok
}

// ListSites returns all registered site IDs.
func (s *Server) ListSites() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sites := make([]string, 0, len(s.devices))
	for site := range s.devices {
		sites = append(sites, site)
	}
	return sites
}

// ListDevices returns all device IDs for a site.
func (s *Server) ListDevices(siteID string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	site, ok := s.devices[siteID]
	if !ok {
		return nil
	}

	devices := make([]string, 0, len(site))
	for id := range site {
		devices = append(devices, id)
	}
	return devices
}

// ListImportTariffs returns all import tariff IDs.
func (s *Server) ListImportTariffs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.importTariffs))
	for id := range s.importTariffs {
		ids = append(ids, id)
	}
	return ids
}

// ListExportTariffs returns all export tariff IDs.
func (s *Server) ListExportTariffs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.exportTariffs))
	for id := range s.exportTariffs {
		ids = append(ids, id)
	}
	return ids
}

// HandleMessage processes a single JSON-RPC message.
func (s *Server) HandleMessage(ctx context.Context, msg []byte) *Response {
	var req Request
	if err := json.Unmarshal(msg, &req); err != nil {
		return errorResponse(nil, ErrParseError, fmt.Sprintf("parse error: %v", err))
	}

	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, ErrInvalidRequest, "invalid jsonrpc version")
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req.ID, req.Params)
	case "initialized":
		// Notification, no response needed
		return nil
	case "resources/list":
		return s.handleResourcesList(req.ID)
	case "resources/read":
		return s.handleResourcesRead(ctx, req.ID, req.Params)
	case "tools/list":
		return s.handleToolsList(req.ID)
	case "tools/call":
		return s.handleToolsCall(ctx, req.ID, req.Params)
	case "ping":
		return successResponse(req.ID, struct{}{})
	default:
		return errorResponse(req.ID, ErrMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *Server) handleInitialize(id interface{}, params json.RawMessage) *Response {
	var initParams InitializeParams
	if err := json.Unmarshal(params, &initParams); err != nil {
		return errorResponse(id, ErrInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	result := InitializeResult{
		ProtocolVersion: ProtocolVersion,
		Capabilities: ServerCapabilities{
			Resources: &ResourcesCapability{ListChanged: false},
			Tools:     &ToolsCapability{ListChanged: false},
		},
		ServerInfo: Implementation{
			Name:    ServerName,
			Version: ServerVersion,
		},
	}

	return successResponse(id, result)
}

func (s *Server) handleResourcesList(id interface{}) *Response {
	resources := s.buildResourceList()
	return successResponse(id, ListResourcesResult{Resources: resources})
}

func (s *Server) buildResourceList() []Resource {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var resources []Resource

	// Device resources
	for siteID, devices := range s.devices {
		for deviceID := range devices {
			// Day resource template
			resources = append(resources, Resource{
				URI:         fmt.Sprintf("energy://site/%s/device/%s/day/{YYYY-MM-DD}", siteID, deviceID),
				Name:        fmt.Sprintf("%s/%s Daily Data", siteID, deviceID),
				Description: fmt.Sprintf("Daily energy data for device %s at site %s", deviceID, siteID),
				MimeType:    "application/json",
			})

			// Range resource template
			resources = append(resources, Resource{
				URI:         fmt.Sprintf("energy://site/%s/device/%s/range/{from}/{to}", siteID, deviceID),
				Name:        fmt.Sprintf("%s/%s Range Data", siteID, deviceID),
				Description: fmt.Sprintf("Date range energy data for device %s at site %s", deviceID, siteID),
				MimeType:    "application/json",
			})

			// Meta resource
			resources = append(resources, Resource{
				URI:         fmt.Sprintf("energy://site/%s/device/%s/meta", siteID, deviceID),
				Name:        fmt.Sprintf("%s/%s Metadata", siteID, deviceID),
				Description: fmt.Sprintf("Device metadata for %s at site %s", deviceID, siteID),
				MimeType:    "application/json",
			})
		}
	}

	// Discovery resources
	resources = append(resources, Resource{
		URI:         "energy://sites",
		Name:        "Sites",
		Description: "List of available site IDs",
		MimeType:    "application/json",
	})

	// Tariff resources
	for id := range s.importTariffs {
		resources = append(resources, Resource{
			URI:         fmt.Sprintf("energy://tariff/import/%s/rates/{from}/{to}", id),
			Name:        fmt.Sprintf("Import Tariff %s Rates", id),
			Description: fmt.Sprintf("Import rates for tariff %s", id),
			MimeType:    "application/json",
		})
	}

	for id := range s.exportTariffs {
		resources = append(resources, Resource{
			URI:         fmt.Sprintf("energy://tariff/export/%s/rates/{from}/{to}", id),
			Name:        fmt.Sprintf("Export Tariff %s Rates", id),
			Description: fmt.Sprintf("Export rates for tariff %s", id),
			MimeType:    "application/json",
		})
	}

	return resources
}

func (s *Server) handleResourcesRead(ctx context.Context, id interface{}, params json.RawMessage) *Response {
	var readParams ReadResourceParams
	if err := json.Unmarshal(params, &readParams); err != nil {
		return errorResponse(id, ErrInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	contents, err := s.readResource(ctx, readParams.URI)
	if err != nil {
		return errorResponse(id, ErrInternalError, err.Error())
	}

	return successResponse(id, ReadResourceResult{Contents: contents})
}

func (s *Server) readResource(ctx context.Context, uri string) ([]ResourceContents, error) {
	// Parse URI and route to appropriate handler
	if strings.HasPrefix(uri, "energy://site/") {
		return s.readDeviceResource(ctx, uri)
	}
	if strings.HasPrefix(uri, "energy://tariff/") {
		return s.readTariffResource(ctx, uri)
	}
	if uri == "energy://sites" {
		return s.readSitesResource()
	}

	return nil, fmt.Errorf("unknown resource URI: %s", uri)
}

func (s *Server) readSitesResource() ([]ResourceContents, error) {
	sites := s.ListSites()
	data, err := json.Marshal(sites)
	if err != nil {
		return nil, err
	}

	return []ResourceContents{{
		URI:      "energy://sites",
		MimeType: "application/json",
		Text:     string(data),
	}}, nil
}

func (s *Server) handleToolsList(id interface{}) *Response {
	tools := s.buildToolList()
	return successResponse(id, ListToolsResult{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, id interface{}, params json.RawMessage) *Response {
	var callParams CallToolParams
	if err := json.Unmarshal(params, &callParams); err != nil {
		return errorResponse(id, ErrInvalidParams, fmt.Sprintf("invalid params: %v", err))
	}

	result, err := s.callTool(ctx, callParams.Name, callParams.Arguments)
	if err != nil {
		return errorResponse(id, ErrInternalError, err.Error())
	}

	return successResponse(id, result)
}

// Helper method for date parsing.
func parseDate(dateStr string) (time.Time, error) {
	return time.Parse("2006-01-02", dateStr)
}

// logError logs errors at appropriate level.
func logError(format string, args ...interface{}) {
	log.Printf("[MCP] "+format, args...)
}

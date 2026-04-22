// Package registry provides metadata storage for devices and tariffs.
package registry

import (
	"context"
	"fmt"
	"log"
	"time"

	"energy-utility/internal/store"
)

// DeviceRegistry stores metadata about available devices.
type DeviceRegistry struct {
	Version   string        `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
	Devices   []DeviceEntry `json:"devices"`
}

// DeviceEntry represents a single device in the registry.
type DeviceEntry struct {
	SiteID       string     `json:"site_id"`
	DeviceID     string     `json:"device_id"`
	Type         string     `json:"type"` // "solax", etc.
	Manufacturer string     `json:"manufacturer"`
	Model        string     `json:"model"`
	DataSource   DataSource `json:"data_source"`
	FirstData    time.Time  `json:"first_data"` // Date of earliest data
	LastData     time.Time  `json:"last_data"`  // Date of latest data
	DataCount    int        `json:"data_count"` // Number of days with data
}

// DataSource describes where the device data is stored.
type DataSource struct {
	Type   string `json:"type"`   // "s3", "local", etc.
	Path   string `json:"path"`   // Path pattern, e.g., "solax/raw/"
	Format string `json:"format"` // "daily-detail", etc.
}

// TariffRegistry stores metadata about available tariffs.
type TariffRegistry struct {
	Version   string        `json:"version"`
	UpdatedAt time.Time     `json:"updated_at"`
	Import    []TariffEntry `json:"import"`
	Export    []TariffEntry `json:"export"`
}

// TariffEntry represents a single tariff in the registry.
type TariffEntry struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Code           string     `json:"code"`
	Type           string     `json:"type"`      // "fixed", "time_of_use", "dynamic"
	Direction      string     `json:"direction"` // "import" or "export"
	Provider       string     `json:"provider"`  // "octopus", etc.
	DataSource     DataSource `json:"data_source"`
	ActiveFrom     time.Time  `json:"active_from"`
	ActiveTo       *time.Time `json:"active_to,omitempty"`
	StandingCharge *float64   `json:"standing_charge,omitempty"` // p/day
}

const (
	devicesKey      = "registry/devices.json"
	tariffsKey      = "registry/tariffs.json"
	registryVersion = "1.0"
)

// LoadDeviceRegistry loads the device registry from storage.
func LoadDeviceRegistry(ctx context.Context, s store.Store) (*DeviceRegistry, error) {
	var reg DeviceRegistry
	if err := s.GetJSON(ctx, devicesKey, &reg); err != nil {
		if store.IsNotFound(err) {
			// Return empty registry if not found
			return &DeviceRegistry{
				Version:   registryVersion,
				UpdatedAt: time.Now(),
				Devices:   []DeviceEntry{},
			}, nil
		}
		return nil, fmt.Errorf("load device registry: %w", err)
	}
	return &reg, nil
}

// SaveDeviceRegistry saves the device registry to storage.
func SaveDeviceRegistry(ctx context.Context, s store.Store, reg *DeviceRegistry) error {
	reg.Version = registryVersion
	reg.UpdatedAt = time.Now()
	if err := s.PutJSON(ctx, devicesKey, reg); err != nil {
		return fmt.Errorf("save device registry: %w", err)
	}
	return nil
}

// LoadTariffRegistry loads the tariff registry from storage.
func LoadTariffRegistry(ctx context.Context, s store.Store) (*TariffRegistry, error) {
	var reg TariffRegistry
	if err := s.GetJSON(ctx, tariffsKey, &reg); err != nil {
		if store.IsNotFound(err) {
			// Return empty registry if not found
			return &TariffRegistry{
				Version:   registryVersion,
				UpdatedAt: time.Now(),
				Import:    []TariffEntry{},
				Export:    []TariffEntry{},
			}, nil
		}
		return nil, fmt.Errorf("load tariff registry: %w", err)
	}
	return &reg, nil
}

// SaveTariffRegistry saves the tariff registry to storage.
func SaveTariffRegistry(ctx context.Context, s store.Store, reg *TariffRegistry) error {
	reg.Version = registryVersion
	reg.UpdatedAt = time.Now()
	if err := s.PutJSON(ctx, tariffsKey, reg); err != nil {
		return fmt.Errorf("save tariff registry: %w", err)
	}
	return nil
}

// AddOrUpdateDevice adds a device to the registry or updates existing.
func (r *DeviceRegistry) AddOrUpdateDevice(dev DeviceEntry) {
	for i, existing := range r.Devices {
		if existing.SiteID == dev.SiteID && existing.DeviceID == dev.DeviceID {
			r.Devices[i] = dev
			return
		}
	}
	r.Devices = append(r.Devices, dev)
}

// GetDevice retrieves a device by site and device ID.
func (r *DeviceRegistry) GetDevice(siteID, deviceID string) (*DeviceEntry, bool) {
	for _, dev := range r.Devices {
		if dev.SiteID == siteID && dev.DeviceID == deviceID {
			return &dev, true
		}
	}
	return nil, false
}

// ListDevices returns all devices, optionally filtered by site.
func (r *DeviceRegistry) ListDevices(siteID string) []DeviceEntry {
	if siteID == "" {
		return r.Devices
	}
	var filtered []DeviceEntry
	for _, dev := range r.Devices {
		if dev.SiteID == siteID {
			filtered = append(filtered, dev)
		}
	}
	return filtered
}

// ListSites returns all unique site IDs.
func (r *DeviceRegistry) ListSites() []string {
	siteMap := make(map[string]bool)
	for _, dev := range r.Devices {
		siteMap[dev.SiteID] = true
	}
	sites := make([]string, 0, len(siteMap))
	for site := range siteMap {
		sites = append(sites, site)
	}
	return sites
}

// AddOrUpdateTariff adds a tariff to the registry or updates existing.
func (r *TariffRegistry) AddOrUpdateTariff(t TariffEntry) {
	list := &r.Import
	if t.Direction == "export" {
		list = &r.Export
	}

	for i, existing := range *list {
		if existing.ID == t.ID {
			(*list)[i] = t
			return
		}
	}
	*list = append(*list, t)
}

// GetTariff retrieves a tariff by ID and direction.
func (r *TariffRegistry) GetTariff(id, direction string) (*TariffEntry, bool) {
	list := r.Import
	if direction == "export" {
		list = r.Export
	}
	for _, t := range list {
		if t.ID == id {
			return &t, true
		}
	}
	return nil, false
}

// DiscoverDevices scans storage and populates device registry from existing data.
// This is useful for migrating from the old structure.
func DiscoverDevices(ctx context.Context, s store.Store) (*DeviceRegistry, error) {
	reg := &DeviceRegistry{
		Version:   registryVersion,
		UpdatedAt: time.Now(),
	}

	// For now, we auto-discover SolaX devices from the raw data path
	// In the future, this could scan for multiple device types
	keys, err := s.List(ctx, "solax/raw/")
	if err != nil {
		return nil, fmt.Errorf("list solax data: %w", err)
	}

	// Count days and find date range
	dataCount := len(keys)
	var firstData, lastData time.Time

	for _, key := range keys {
		// Extract date from key: solax/raw/YYYY/MM/DD/daily-detail.json
		var year, month, day int
		if _, err := fmt.Sscanf(key, "solax/raw/%4d/%2d/%2d/", &year, &month, &day); err == nil {
			t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
			if firstData.IsZero() || t.Before(firstData) {
				firstData = t
			}
			if t.After(lastData) {
				lastData = t
			}
		}
	}

	if dataCount > 0 {
		reg.Devices = append(reg.Devices, DeviceEntry{
			SiteID:       "home",
			DeviceID:     "inverter-1",
			Type:         "solax",
			Manufacturer: "SolaX",
			Model:        "X3-Hybrid",
			DataSource: DataSource{
				Type:   "s3",
				Path:   "solax/raw/",
				Format: "daily-detail",
			},
			FirstData: firstData,
			LastData:  lastData,
			DataCount: dataCount,
		})
	}

	return reg, nil
}

// DiscoverTariffs scans storage and populates tariff registry from existing data.
func DiscoverTariffs(ctx context.Context, s store.Store) (*TariffRegistry, error) {
	reg := &TariffRegistry{
		Version:   registryVersion,
		UpdatedAt: time.Now(),
	}

	// Check for import rates - support both old and new path structures
	importKeys, _ := s.List(ctx, "octopus/agile-import/")
	if len(importKeys) == 0 {
		// Try alternative path structure
		importKeys, _ = s.List(ctx, "octopus/agile/import/")
	}
	if len(importKeys) > 0 {
		// Try to extract actual tariff code from the data
		code := "E-1R-AGILE-24-10-01-C" // Default
		path := "octopus/agile-import/"
		if len(importKeys) > 0 && len(importKeys[0]) > 0 {
			// Use the actual path found
			if importKeys[0][0:23] == "octopus/agile/import/" {
				path = "octopus/agile/import/"
			}
		}
		reg.Import = append(reg.Import, TariffEntry{
			ID:        "agile-import",
			Name:      "Octopus Agile Import",
			Code:      code,
			Type:      "dynamic",
			Direction: "import",
			Provider:  "octopus",
			DataSource: DataSource{
				Type:   "s3",
				Path:   path,
				Format: "monthly-rates",
			},
		})
	}

	// Check for export rates - support both old and new path structures
	exportKeys, _ := s.List(ctx, "octopus/agile-export/")
	if len(exportKeys) == 0 {
		// Try alternative path structure
		exportKeys, _ = s.List(ctx, "octopus/agile/export/")
	}
	if len(exportKeys) > 0 {
		code := "E-1R-OUTGOING-24-09-01-C" // Default
		path := "octopus/agile-export/"
		if len(exportKeys) > 0 && len(exportKeys[0]) > 0 {
			if exportKeys[0][0:23] == "octopus/agile/export/" {
				path = "octopus/agile/export/"
			}
		}
		reg.Export = append(reg.Export, TariffEntry{
			ID:        "agile-export",
			Name:      "Octopus Agile Export",
			Code:      code,
			Type:      "dynamic",
			Direction: "export",
			Provider:  "octopus",
			DataSource: DataSource{
				Type:   "s3",
				Path:   path,
				Format: "monthly-rates",
			},
		})
	}

	return reg, nil
}

// EnsureRegistries creates registries if they don't exist.
// This is useful for first-run initialization.
func EnsureRegistries(ctx context.Context, s store.Store) error {
	// Check device registry
	deviceExists, err := s.Exists(ctx, devicesKey)
	if err != nil {
		return fmt.Errorf("check device registry: %w", err)
	}

	if !deviceExists {
		log.Println("Registry: device registry not found, discovering from storage...")
		reg, err := DiscoverDevices(ctx, s)
		if err != nil {
			return fmt.Errorf("discover devices: %w", err)
		}
		if len(reg.Devices) > 0 {
			if err := SaveDeviceRegistry(ctx, s, reg); err != nil {
				return fmt.Errorf("save device registry: %w", err)
			}
			log.Printf("Registry: created device registry with %d device(s)", len(reg.Devices))
		} else {
			log.Println("Registry: no devices found in storage")
		}
	}

	// Check tariff registry
	tariffExists, err := s.Exists(ctx, tariffsKey)
	if err != nil {
		return fmt.Errorf("check tariff registry: %w", err)
	}

	if !tariffExists {
		log.Println("Registry: tariff registry not found, discovering from storage...")
		reg, err := DiscoverTariffs(ctx, s)
		if err != nil {
			return fmt.Errorf("discover tariffs: %w", err)
		}
		total := len(reg.Import) + len(reg.Export)
		if total > 0 {
			if err := SaveTariffRegistry(ctx, s, reg); err != nil {
				return fmt.Errorf("save tariff registry: %w", err)
			}
			log.Printf("Registry: created tariff registry with %d tariff(s)", total)
		} else {
			log.Println("Registry: no tariffs found in storage")
		}
	}

	return nil
}

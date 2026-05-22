package mcp

import (
	"context"
	"fmt"
	"log"
	"path"
	"strings"

	"energy-utility/internal/config"
	"energy-utility/internal/device/solax"
	"energy-utility/internal/registry"
	"energy-utility/internal/store"
	"energy-utility/internal/tariff"
	octopusstore "energy-utility/internal/tariff/octopus"
)

// ConfigureServer sets up the MCP server with devices and tariffs.
func ConfigureServer(ctx context.Context, server *Server, s store.Store, cfg *config.Config) error {
	// Load or discover devices
	deviceCount := 0
	if cfg.MCP.AutoDiscover {
		// Try to load from registry first
		reg, err := registry.LoadDeviceRegistry(ctx, s)
		if err != nil {
			log.Printf("MCP: could not load device registry, discovering from storage...")
			reg, err = registry.DiscoverDevices(ctx, s)
			if err != nil {
				return fmt.Errorf("discover devices: %w", err)
			}
			// Save discovered registry for next time
			if err := registry.SaveDeviceRegistry(ctx, s, reg); err != nil {
				log.Printf("MCP: warning: could not save device registry: %v", err)
			}
		}

		for _, dev := range reg.Devices {
			switch dev.Type {
			case "solax":
				solaxDevice := solax.NewMCPDevice(dev.SiteID, s)
				server.RegisterDevice(dev.SiteID, dev.DeviceID, solaxDevice)
				log.Printf("MCP: registered SolaX device %s/%s (%d days of data)",
					dev.SiteID, dev.DeviceID, dev.DataCount)
				deviceCount++
			default:
				log.Printf("MCP: skipping unknown device type %q for %s/%s", dev.Type, dev.SiteID, dev.DeviceID)
			}
		}
	}

	// Also register any manually configured devices
	for _, devCfg := range cfg.MCP.Devices {
		switch devCfg.Type {
		case "solax":
			solaxDevice := solax.NewMCPDevice(devCfg.SiteID, s)
			server.RegisterDevice(devCfg.SiteID, devCfg.DeviceID, solaxDevice)
			log.Printf("MCP: registered configured SolaX device %s/%s", devCfg.SiteID, devCfg.DeviceID)
			deviceCount++
		default:
			log.Printf("MCP: unknown device type %q for %s/%s", devCfg.Type, devCfg.SiteID, devCfg.DeviceID)
		}
	}

	// Load or discover tariffs
	importCount, exportCount := 0, 0

	// Try to load from registry first
	tariffReg, err := registry.LoadTariffRegistry(ctx, s)
	if err != nil {
		log.Printf("MCP: could not load tariff registry, discovering from storage...")
		tariffReg, err = registry.DiscoverTariffs(ctx, s)
		if err != nil {
			log.Printf("MCP: warning: could not discover tariffs: %v", err)
		} else {
			// Save discovered registry for next time
			if err := registry.SaveTariffRegistry(ctx, s, tariffReg); err != nil {
				log.Printf("MCP: warning: could not save tariff registry: %v", err)
			}
		}
	}

	// Register discovered/import tariffs
	for _, t := range tariffReg.Import {
		region := extractRegionFromPath(t.DataSource.Path)
		importTariff := octopusstore.NewMCPImportTariff(
			t.ID,
			t.Name,
			t.Code,
			parseTariffType(t.Type),
			s,
			region,
			0.0,
		)
		server.RegisterImportTariff(t.ID, importTariff)
		log.Printf("MCP: registered import tariff %s (%s, region=%s)", t.ID, t.Name, region)
		importCount++
	}

	// Register discovered/export tariffs
	for _, t := range tariffReg.Export {
		region := extractRegionFromPath(t.DataSource.Path)
		exportTariff := octopusstore.NewMCPExportTariff(
			t.ID,
			t.Name,
			t.Code,
			parseTariffType(t.Type),
			s,
			region,
		)
		server.RegisterExportTariff(t.ID, exportTariff)
		log.Printf("MCP: registered export tariff %s (%s, region=%s)", t.ID, t.Name, region)
		exportCount++
	}

	// Also register any manually configured tariffs
	for _, tCfg := range cfg.MCP.Tariffs.Import {
		region := extractRegionFromPath(tCfg.StorageKey)
		if region == "" {
			region = cfg.Octopus.Region
		}
		importTariff := octopusstore.NewMCPImportTariff(
			tCfg.ID,
			tCfg.Name,
			tCfg.Code,
			parseTariffType(tCfg.Type),
			s,
			region,
			0.0,
		)
		server.RegisterImportTariff(tCfg.ID, importTariff)
		log.Printf("MCP: registered configured import tariff %s (region=%s)", tCfg.ID, region)
		importCount++
	}

	for _, tCfg := range cfg.MCP.Tariffs.Export {
		region := extractRegionFromPath(tCfg.StorageKey)
		if region == "" {
			region = cfg.Octopus.Region
		}
		exportTariff := octopusstore.NewMCPExportTariff(
			tCfg.ID,
			tCfg.Name,
			tCfg.Code,
			parseTariffType(tCfg.Type),
			s,
			region,
		)
		server.RegisterExportTariff(tCfg.ID, exportTariff)
		log.Printf("MCP: registered configured export tariff %s (region=%s)", tCfg.ID, region)
		exportCount++
	}

	log.Printf("MCP server configured: %d devices, %d import tariffs, %d export tariffs",
		deviceCount, importCount, exportCount)

	return nil
}

// parseTariffType converts a string tariff type to the enum value.
func parseTariffType(t string) tariff.TariffType {
	switch strings.ToLower(t) {
	case "fixed":
		return tariff.TariffFixed
	case "time_of_use", "time-of-use":
		return tariff.TariffTimeOfUse
	case "dynamic":
		return tariff.TariffDynamic
	default:
		return tariff.TariffFixed
	}
}

// extractRegionFromPath extracts a single-letter region code from an agile
// storage path such as "octopus/agile-import/E/" or "octopus/agile-export/E/".
// It returns an empty string when no region segment is present.
func extractRegionFromPath(p string) string {
	p = strings.TrimSuffix(p, "/")
	// Expecting paths like "octopus/agile-import/E"
	if strings.HasPrefix(p, "octopus/agile-") {
		base := path.Base(p)
		if len(base) == 1 && base >= "A" && base <= "P" {
			return base
		}
	}
	return ""
}

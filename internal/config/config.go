package config

import (
	"fmt"
	"os"
	"time"

	"energy-utility/internal/device"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Solax    SolaxConfig   `yaml:"solax"`
	Octopus  OctopusConfig `yaml:"octopus"`
	Battery  BatteryConfig `yaml:"battery"`
	S3       S3Config      `yaml:"s3"`
	CacheDir string        `yaml:"cache_dir"`
	MCP      MCPConfig     `yaml:"mcp"`
}

type MCPConfig struct {
	Enabled      bool              `yaml:"enabled"`
	Path         string            `yaml:"path"`
	AutoDiscover bool              `yaml:"auto_discover"`
	Devices      []MCPDeviceConfig `yaml:"devices,omitempty"`
	Tariffs      MCPTariffsConfig  `yaml:"tariffs"`
}

type MCPDeviceConfig struct {
	SiteID   string `yaml:"site_id"`
	DeviceID string `yaml:"device_id"`
	Type     string `yaml:"type"`
}

type MCPTariffsConfig struct {
	Import []MCPTariffConfig `yaml:"import"`
	Export []MCPTariffConfig `yaml:"export"`
}

type MCPTariffConfig struct {
	ID         string `yaml:"id"`
	Name       string `yaml:"name"`
	StorageKey string `yaml:"storage_key"`
	Code       string `yaml:"code,omitempty"`
	Type       string `yaml:"type,omitempty"`
}

type BatteryConfig struct {
	CapacityKWh         float64 `yaml:"capacity_kwh"`
	MaxChargeKW         float64 `yaml:"max_charge_kw"`
	MaxDischargeKW      float64 `yaml:"max_discharge_kw"`
	MinSoCPercent       float64 `yaml:"min_soc_percent"`
	MaxSoCPercent       float64 `yaml:"max_soc_percent"`
	RoundTripEfficiency float64 `yaml:"round_trip_efficiency"`
	CycleCostPence      float64 `yaml:"cycle_cost_pence"`
}

func (c *BatteryConfig) ToDeviceConfig() device.BatteryConfig {
	return device.BatteryConfig{
		CapacityKWh:         c.CapacityKWh,
		MaxChargeKW:         c.MaxChargeKW,
		MaxDischargeKW:      c.MaxDischargeKW,
		MinSoCPercent:       c.MinSoCPercent,
		MaxSoCPercent:       c.MaxSoCPercent,
		RoundTripEfficiency: c.RoundTripEfficiency,
		CycleCostPence:      c.CycleCostPence,
	}
}

type SolaxConfig struct {
	Email     string `yaml:"email"`
	Password  string `yaml:"password"`
	SiteID    string `yaml:"site_id"`
	CryptoKey string `yaml:"crypto_key"`
	CryptoIV  string `yaml:"crypto_iv"`
}

type OctopusConfig struct {
	APIKey            string       `yaml:"api_key"`
	AccountID         string       `yaml:"account_id"`
	MPANImport        string       `yaml:"mpan_import"`
	MPANExport        string       `yaml:"mpan_export"`
	MeterSerialImport string       `yaml:"meter_serial_import"`
	MeterSerialExport string       `yaml:"meter_serial_export"`
	Region            string       `yaml:"region"`
	GoRates           []GoRate     `yaml:"go_rates"`
	ExportRates       []ExportRate `yaml:"export_rates"`
}

type GoRate struct {
	From           string  `yaml:"from"`
	PeakRate       float64 `yaml:"peak_rate"`
	OffpeakRate    float64 `yaml:"offpeak_rate"`
	OffpeakStart   string  `yaml:"offpeak_start"`
	OffpeakEnd     string  `yaml:"offpeak_end"`
	StandingCharge float64 `yaml:"standing_charge"`
}

type ExportRate struct {
	From string  `yaml:"from"`
	Rate float64 `yaml:"rate"`
}

type S3Config struct {
	Endpoint string `yaml:"endpoint"`
	Bucket   string `yaml:"bucket"`
	Region   string `yaml:"region"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	cfg.applyEnv()
	return &cfg, nil
}

// applyEnv overrides sensitive config fields from environment variables.
//
//	SOLAX_EMAIL           solax.email
//	SOLAX_PASSWORD        solax.password
//	SOLAX_SITE_ID         solax.site_id
//	SOLAX_CRYPTO_KEY      solax.crypto_key
//	SOLAX_CRYPTO_IV       solax.crypto_iv
//	OCTOPUS_API_KEY       octopus.api_key
//	OCTOPUS_ACCOUNT_ID    octopus.account_id
//	S3_ENDPOINT           s3.endpoint
//	CACHE_DIR             cache_dir (default: .cache)
//	AWS_ACCESS_KEY_ID     S3 credentials (SDK)
//	AWS_SECRET_ACCESS_KEY S3 credentials (SDK)
func (c *Config) applyEnv() {
	overrideStr := func(dest *string, key string) {
		if v := os.Getenv(key); v != "" {
			*dest = v
		}
	}
	overrideStr(&c.Solax.Email, "SOLAX_EMAIL")
	overrideStr(&c.Solax.Password, "SOLAX_PASSWORD")
	overrideStr(&c.Solax.SiteID, "SOLAX_SITE_ID")
	overrideStr(&c.Solax.CryptoKey, "SOLAX_CRYPTO_KEY")
	overrideStr(&c.Solax.CryptoIV, "SOLAX_CRYPTO_IV")
	overrideStr(&c.Octopus.APIKey, "OCTOPUS_API_KEY")
	overrideStr(&c.Octopus.AccountID, "OCTOPUS_ACCOUNT_ID")
	overrideStr(&c.S3.Endpoint, "S3_ENDPOINT")
	overrideStr(&c.CacheDir, "CACHE_DIR")
	if c.CacheDir == "" {
		c.CacheDir = ".cache"
	}
	c.applyBatteryDefaults()
	c.applyMCPDefaults()
}

func (c *Config) applyMCPDefaults() {
	if c.MCP.Path == "" {
		c.MCP.Path = "/mcp"
	}
}

func (c *Config) applyBatteryDefaults() {
	if c.Battery.CapacityKWh == 0 {
		c.Battery.CapacityKWh = 12.0
	}
	if c.Battery.MaxChargeKW == 0 {
		c.Battery.MaxChargeKW = 7.5
	}
	if c.Battery.MaxDischargeKW == 0 {
		c.Battery.MaxDischargeKW = 7.5
	}
	if c.Battery.MinSoCPercent == 0 {
		c.Battery.MinSoCPercent = 10.0
	}
	if c.Battery.MaxSoCPercent == 0 {
		c.Battery.MaxSoCPercent = 100.0
	}
	if c.Battery.RoundTripEfficiency == 0 {
		c.Battery.RoundTripEfficiency = 0.90
	}
	if c.Battery.CycleCostPence == 0 {
		c.Battery.CycleCostPence = 80.0
	}
}

// GoRateAt returns the Go tariff rate in effect on the given date.
func (c *OctopusConfig) GoRateAt(d time.Time) *GoRate {
	var result *GoRate
	for i := range c.GoRates {
		from, err := time.Parse("2006-01-02", c.GoRates[i].From)
		if err != nil {
			continue
		}
		if !d.Before(from) {
			result = &c.GoRates[i]
		}
	}
	return result
}

// ExportRateAt returns the export rate in p/kWh in effect on the given date.
func (c *OctopusConfig) ExportRateAt(d time.Time) float64 {
	var rate float64
	for _, r := range c.ExportRates {
		from, err := time.Parse("2006-01-02", r.From)
		if err != nil {
			continue
		}
		if !d.Before(from) {
			rate = r.Rate
		}
	}
	return rate
}

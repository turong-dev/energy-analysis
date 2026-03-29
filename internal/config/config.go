package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Solax    SolaxConfig   `yaml:"solax"`
	Octopus  OctopusConfig `yaml:"octopus"`
	S3       S3Config      `yaml:"s3"`
	CacheDir string        `yaml:"cache_dir"`
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
	overrideStr(&c.S3.Endpoint, "S3_ENDPOINT")
	overrideStr(&c.CacheDir, "CACHE_DIR")
	if c.CacheDir == "" {
		c.CacheDir = ".cache"
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

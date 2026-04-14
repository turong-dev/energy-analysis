package tariff

import "time"

type Rate struct {
	ValueIncVAT float64
	ValidFrom   time.Time
	ValidTo     time.Time
}

type TariffType int

const (
	TariffFixed TariffType = iota
	TariffTimeOfUse
	TariffDynamic
)

type ImportTariff interface {
	Name() string
	Code() string
	Type() TariffType
	Rate(t time.Time) (Rate, error)
	Rates(from, to time.Time) ([]Rate, error)
	StandingCharge(t time.Time) (float64, error)
}

type ExportTariff interface {
	Name() string
	Code() string
	Type() TariffType
	Rate(t time.Time) (Rate, error)
	Rates(from, to time.Time) ([]Rate, error)
}

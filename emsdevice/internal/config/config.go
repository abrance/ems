package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
)

const (
	ModeStatic = "static"
	ModeLive   = "live"
)

type Config struct {
	Listen string
	UnitID uint
	Mock   Mock
}

type Mock struct {
	Mode        string
	Interval    time.Duration
	SOCJitter   float64
	PowerJitter float64
	Values      essdemo.Values
}

type fileConfig struct {
	Listen *string   `json:"listen"`
	UnitID *uint     `json:"unitId"`
	Mock   *fileMock `json:"mock"`
}

type fileMock struct {
	Mode              *string  `json:"mode"`
	Interval          *string  `json:"interval"`
	SOCJitter         *float64 `json:"socJitter"`
	PowerJitter       *float64 `json:"powerJitter"`
	Status            *int     `json:"status"`
	Power             *float64 `json:"power"`
	SOC               *float64 `json:"soc"`
	GeneratedEnergy   *float64 `json:"generatedEnergy"`
	ConsumedEnergy    *float64 `json:"consumedEnergy"`
	MaxChargePower    *float64 `json:"maxChargePower"`
	MaxDischargePower *float64 `json:"maxDischargePower"`
	DeviceControl     *int     `json:"deviceControl"`
	TargetPower       *float64 `json:"targetPower"`
	PowerCapacity     *float64 `json:"powerCapacity"`
	EnergyCapacity    *float64 `json:"energyCapacity"`
	MinSOC            *float64 `json:"minSoc"`
	MaxSOC            *float64 `json:"maxSoc"`
	ChargeEfficiency  *float64 `json:"chargeEfficiency"`
}

type Overrides struct {
	Listen      string
	UnitID      *uint
	Mode        string
	Interval    string
	SOC         *float64
	Power       *float64
	Status      *int
	Control     *int
	TargetPower *float64
	SOCJitter   *float64
	PowerJitter *float64
}

func Default() Config {
	return Config{
		Listen: "tcp://0.0.0.0:1502",
		UnitID: 1,
		Mock: Mock{
			Mode:        ModeStatic,
			Interval:    time.Second,
			SOCJitter:   2,
			PowerJitter: 1.5,
			Values:      essdemo.DefaultValues(),
		},
	}
}

func Load(path string, ov Overrides) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := applyOverrides(&cfg, ov); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch c.Mock.Mode {
	case ModeStatic, ModeLive:
	default:
		return fmt.Errorf("mock.mode must be %s or %s", ModeStatic, ModeLive)
	}
	if c.Mock.Interval <= 0 {
		return fmt.Errorf("mock.interval must be > 0")
	}
	return nil
}

func applyFile(cfg *Config, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var file fileConfig
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if file.Listen != nil {
		cfg.Listen = *file.Listen
	}
	if file.UnitID != nil {
		cfg.UnitID = *file.UnitID
	}
	if file.Mock != nil {
		if err := applyFileMock(&cfg.Mock, file.Mock); err != nil {
			return err
		}
	}
	return nil
}

func applyFileMock(dst *Mock, src *fileMock) error {
	if src.Mode != nil {
		mode, err := NormalizeMode(*src.Mode)
		if err != nil {
			return err
		}
		dst.Mode = mode
	}
	if src.Interval != nil {
		d, err := time.ParseDuration(*src.Interval)
		if err != nil {
			return fmt.Errorf("mock.interval: %w", err)
		}
		dst.Interval = d
	}
	if src.SOCJitter != nil {
		dst.SOCJitter = *src.SOCJitter
	}
	if src.PowerJitter != nil {
		dst.PowerJitter = *src.PowerJitter
	}
	overlayValues(&dst.Values, src)
	return nil
}

func overlayValues(dst *essdemo.Values, src *fileMock) {
	if src.Status != nil {
		dst.Status = *src.Status
	}
	if src.Power != nil {
		dst.Power = *src.Power
	}
	if src.SOC != nil {
		dst.SOC = *src.SOC
	}
	if src.GeneratedEnergy != nil {
		dst.GeneratedEnergy = *src.GeneratedEnergy
	}
	if src.ConsumedEnergy != nil {
		dst.ConsumedEnergy = *src.ConsumedEnergy
	}
	if src.MaxChargePower != nil {
		dst.MaxChargePower = *src.MaxChargePower
	}
	if src.MaxDischargePower != nil {
		dst.MaxDischargePower = *src.MaxDischargePower
	}
	if src.DeviceControl != nil {
		dst.DeviceControl = *src.DeviceControl
	}
	if src.TargetPower != nil {
		dst.TargetPower = *src.TargetPower
	}
	if src.PowerCapacity != nil {
		dst.PowerCapacity = *src.PowerCapacity
	}
	if src.EnergyCapacity != nil {
		dst.EnergyCapacity = *src.EnergyCapacity
	}
	if src.MinSOC != nil {
		dst.MinSOC = *src.MinSOC
	}
	if src.MaxSOC != nil {
		dst.MaxSOC = *src.MaxSOC
	}
	if src.ChargeEfficiency != nil {
		dst.ChargeEfficiency = *src.ChargeEfficiency
	}
}

func applyEnv(cfg *Config) error {
	if v := os.Getenv("EMSDEVICE_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("EMSDEVICE_UNIT_ID"); v != "" {
		n, err := strconv.ParseUint(v, 10, 8)
		if err != nil {
			return fmt.Errorf("EMSDEVICE_UNIT_ID: %w", err)
		}
		cfg.UnitID = uint(n)
	}
	if v := os.Getenv("EMSDEVICE_MOCK"); v != "" {
		mode, err := NormalizeMode(v)
		if err != nil {
			return fmt.Errorf("EMSDEVICE_MOCK: %w", err)
		}
		cfg.Mock.Mode = mode
	}
	if v := os.Getenv("EMSDEVICE_MOCK_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("EMSDEVICE_MOCK_INTERVAL: %w", err)
		}
		cfg.Mock.Interval = d
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_SOC_JITTER", &cfg.Mock.SOCJitter); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_POWER_JITTER", &cfg.Mock.PowerJitter); err != nil {
		return err
	}
	if err := applyEnvInt("EMSDEVICE_MOCK_STATUS", &cfg.Mock.Values.Status); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_POWER", &cfg.Mock.Values.Power); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_SOC", &cfg.Mock.Values.SOC); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_GENERATED_ENERGY", &cfg.Mock.Values.GeneratedEnergy); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_CONSUMED_ENERGY", &cfg.Mock.Values.ConsumedEnergy); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_MAX_CHARGE_POWER", &cfg.Mock.Values.MaxChargePower); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_MAX_DISCHARGE_POWER", &cfg.Mock.Values.MaxDischargePower); err != nil {
		return err
	}
	if err := applyEnvInt("EMSDEVICE_MOCK_CONTROL", &cfg.Mock.Values.DeviceControl); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_TARGET_POWER", &cfg.Mock.Values.TargetPower); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_POWER_CAPACITY", &cfg.Mock.Values.PowerCapacity); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_ENERGY_CAPACITY", &cfg.Mock.Values.EnergyCapacity); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_MIN_SOC", &cfg.Mock.Values.MinSOC); err != nil {
		return err
	}
	if err := applyEnvFloat("EMSDEVICE_MOCK_MAX_SOC", &cfg.Mock.Values.MaxSOC); err != nil {
		return err
	}
	return applyEnvFloat("EMSDEVICE_MOCK_CHARGE_EFFICIENCY", &cfg.Mock.Values.ChargeEfficiency)
}

func applyOverrides(cfg *Config, ov Overrides) error {
	if ov.Listen != "" {
		cfg.Listen = ov.Listen
	}
	if ov.UnitID != nil {
		cfg.UnitID = *ov.UnitID
	}
	if ov.Mode != "" {
		mode, err := NormalizeMode(ov.Mode)
		if err != nil {
			return err
		}
		cfg.Mock.Mode = mode
	}
	if ov.Interval != "" {
		d, err := time.ParseDuration(ov.Interval)
		if err != nil {
			return fmt.Errorf("mock interval: %w", err)
		}
		cfg.Mock.Interval = d
	}
	if ov.SOC != nil {
		cfg.Mock.Values.SOC = *ov.SOC
	}
	if ov.Power != nil {
		cfg.Mock.Values.Power = *ov.Power
	}
	if ov.Status != nil {
		cfg.Mock.Values.Status = *ov.Status
	}
	if ov.Control != nil {
		cfg.Mock.Values.DeviceControl = *ov.Control
	}
	if ov.TargetPower != nil {
		cfg.Mock.Values.TargetPower = *ov.TargetPower
	}
	if ov.SOCJitter != nil {
		cfg.Mock.SOCJitter = *ov.SOCJitter
	}
	if ov.PowerJitter != nil {
		cfg.Mock.PowerJitter = *ov.PowerJitter
	}
	return nil
}

func NormalizeMode(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case ModeStatic, "off", "false", "0":
		return ModeStatic, nil
	case ModeLive, "on", "true", "1":
		return ModeLive, nil
	default:
		return "", fmt.Errorf("unknown mock mode %q", v)
	}
}

func applyEnvFloat(key string, dst *float64) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = n
	return nil
}

func applyEnvInt(key string, dst *int) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	*dst = n
	return nil
}

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadJSONAndFlags(t *testing.T) {
	clearMockEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "emsdevice.json")
	raw := []byte(`{
	  "listen": "tcp://127.0.0.1:2502",
	  "unitId": 3,
	  "mock": {
	    "mode": "live",
	    "interval": "2s",
	    "soc": 55.5,
	    "power": -8,
	    "socJitter": 1.2,
	    "powerJitter": 0.4
	  }
	}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	soc := 70.0
	cfg, err := Load(path, Overrides{SOC: &soc})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "tcp://127.0.0.1:2502" || cfg.UnitID != 3 {
		t.Fatalf("listen/unit %+v", cfg)
	}
	if cfg.Mock.Mode != ModeLive || cfg.Mock.Interval != 2*time.Second {
		t.Fatalf("mock %+v", cfg.Mock)
	}
	if cfg.Mock.Values.SOC != 70 || cfg.Mock.Values.Power != -8 {
		t.Fatalf("values %+v", cfg.Mock.Values)
	}
	if cfg.Mock.Values.Registers()[0xCA] != 700 {
		t.Fatalf("soc register")
	}
}

func TestLoadEnv(t *testing.T) {
	clearMockEnv(t)
	t.Setenv("EMSDEVICE_MOCK", "true")
	t.Setenv("EMSDEVICE_MOCK_SOC", "33")
	t.Setenv("EMSDEVICE_MOCK_POWER", "12.5")
	t.Setenv("EMSDEVICE_UNIT_ID", "4")
	cfg, err := Load("", Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mock.Mode != ModeLive || cfg.UnitID != 4 {
		t.Fatalf("%+v", cfg)
	}
	if cfg.Mock.Values.SOC != 33 || cfg.Mock.Values.Power != 12.5 {
		t.Fatalf("values %+v", cfg.Mock.Values)
	}
}

func TestNormalizeMode(t *testing.T) {
	mode, err := NormalizeMode("ON")
	if err != nil || mode != ModeLive {
		t.Fatalf("%s %v", mode, err)
	}
	mode, err = NormalizeMode("0")
	if err != nil || mode != ModeStatic {
		t.Fatalf("%s %v", mode, err)
	}
	if _, err := NormalizeMode("wave"); err == nil {
		t.Fatal("want error")
	}
}

func clearMockEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"EMSDEVICE_LISTEN", "EMSDEVICE_UNIT_ID", "EMSDEVICE_MOCK",
		"EMSDEVICE_MOCK_INTERVAL", "EMSDEVICE_MOCK_SOC_JITTER", "EMSDEVICE_MOCK_POWER_JITTER",
		"EMSDEVICE_MOCK_STATUS", "EMSDEVICE_MOCK_POWER", "EMSDEVICE_MOCK_SOC",
		"EMSDEVICE_MOCK_GENERATED_ENERGY", "EMSDEVICE_MOCK_CONSUMED_ENERGY",
		"EMSDEVICE_MOCK_MAX_CHARGE_POWER", "EMSDEVICE_MOCK_MAX_DISCHARGE_POWER",
		"EMSDEVICE_MOCK_CONTROL", "EMSDEVICE_MOCK_TARGET_POWER",
		"EMSDEVICE_MOCK_POWER_CAPACITY", "EMSDEVICE_MOCK_ENERGY_CAPACITY",
		"EMSDEVICE_MOCK_MIN_SOC", "EMSDEVICE_MOCK_MAX_SOC", "EMSDEVICE_MOCK_CHARGE_EFFICIENCY",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

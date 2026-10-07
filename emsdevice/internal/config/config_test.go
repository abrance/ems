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

func TestLoadDevicesPointTable(t *testing.T) {
	clearMockEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "emsdevice.json")
	raw := []byte(`{
	  "devices": [
	    {
	      "key": "pv",
	      "name": "光伏",
	      "points": [
	        { "addr": "0x65", "key": "Power", "type": "int16", "scale": 0.1, "value": 60, "jitter": 3, "min": 0, "max": 120 },
	        { "addr": "0x66", "key": "GeneratedEnergy", "type": "uint16", "scale": 0.1, "value": 1200 },
	        { "addr": 104, "key": "PowerLimit", "value": 100, "writable": true }
	      ]
	    }
	  ]
	}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, Overrides{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Devices) != 1 || cfg.Devices[0].Key != "pv" || len(cfg.Devices[0].Points) != 3 {
		t.Fatalf("devices %+v", cfg.Devices)
	}
	power := cfg.Devices[0].Points[0]
	if power.Addr != 0x65 || power.Scale != 0.1 || power.Writable {
		t.Fatalf("power %+v", power)
	}
	if !power.Signed || !power.HasRange || power.Min != 0 || power.Max != 120 {
		t.Fatalf("power range %+v", power)
	}
	energy := cfg.Devices[0].Points[1]
	if energy.Signed {
		t.Fatalf("uint16 must decode as unsigned: %+v", energy)
	}
	decimal := cfg.Devices[0].Points[2]
	if decimal.Addr != 104 || !decimal.Writable || decimal.Scale != 1 {
		t.Fatalf("decimal addr %+v", decimal)
	}
}

func TestLoadDevicesRejectsDuplicateAddress(t *testing.T) {
	clearMockEnv(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "emsdevice.json")
	raw := []byte(`{
	  "devices": [
	    { "key": "a", "points": [ { "addr": "0xC8", "key": "X" } ] },
	    { "key": "b", "points": [ { "addr": 200, "key": "Y" } ] }
	  ]
	}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, Overrides{}); err == nil {
		t.Fatal("duplicate address must fail")
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

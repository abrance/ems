package impl

import (
	"s_db/s_db_model"
	"testing"
)

func TestDeviceModelToConfigJSONFields(t *testing.T) {
	model := &s_db_model.SDeviceModel{
		SDatabaseBasic: s_db_model.SDatabaseBasic{Id: "dev-1"},
		Name:           "ESS Demo",
		Params:         `{"unitId":1}`,
		Enabled:        true,
	}
	cfg := deviceModelToConfig(model)
	if cfg.Id != "dev-1" || cfg.Name != "ESS Demo" || !cfg.Enabled {
		t.Fatalf("basic fields: %+v", cfg)
	}
	unitId, ok := cfg.Params["unitId"]
	if !ok {
		t.Fatalf("missing unitId in params: %+v", cfg.Params)
	}
	if unitId != float64(1) && unitId != 1 {
		t.Fatalf("params unitId=%v (%T)", unitId, unitId)
	}
	if cfg.ExternalParam != nil {
		t.Fatalf("empty external_param should stay nil, got %+v", cfg.ExternalParam)
	}

	model.ExternalParam = `{"modbusId":2,"modbusAllowControl":true}`
	cfg = deviceModelToConfig(model)
	if cfg.ExternalParam == nil || cfg.ExternalParam.ModbusId != 2 || !cfg.ExternalParam.ModbusAllowControl {
		t.Fatalf("external_param: %+v", cfg.ExternalParam)
	}
}

package slave

import (
	"testing"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/simonvetter/modbus"
)

func TestHandleHoldingRegistersReadBlock(t *testing.T) {
	h := NewHandler(1, essdemo.DefaultRegisters())
	got, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId:   1,
		Addr:     essdemo.BaseAddr,
		Quantity: essdemo.Count,
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if uint16(len(got)) != essdemo.Count {
		t.Fatalf("len=%d", len(got))
	}
	if got[0] != 2 || got[2] != 800 {
		t.Fatalf("status=%d soc=%d", got[0], got[2])
	}
}

func TestHandleHoldingRegistersWriteControl(t *testing.T) {
	h := NewHandler(1, essdemo.DefaultRegisters())
	_, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId:   1,
		Addr:     essdemo.AddrDeviceControl,
		Quantity: 1,
		IsWrite:  true,
		Args:     []uint16{0},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if h.Snapshot()[essdemo.AddrDeviceControl] != 0 {
		t.Fatalf("control not updated")
	}
}

func TestHandleHoldingRegistersRejectsRangeAndUnit(t *testing.T) {
	h := NewHandler(1, essdemo.DefaultRegisters())
	if _, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId:   1,
		Addr:     10,
		Quantity: 1,
	}); err != modbus.ErrIllegalDataAddress {
		t.Fatalf("want illegal address, got %v", err)
	}
	if _, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId:   9,
		Addr:     essdemo.AddrSOC,
		Quantity: 1,
	}); err != modbus.ErrIllegalDataAddress {
		t.Fatalf("want unit reject, got %v", err)
	}
}

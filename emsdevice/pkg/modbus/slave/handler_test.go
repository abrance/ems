package slave

import (
	"testing"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/simonvetter/modbus"
)

func TestHandleHoldingRegistersWritableWhitelist(t *testing.T) {
	regs := map[uint16]uint16{essdemo.AddrStatus: 2, essdemo.AddrTargetPower: 50}
	writable := map[uint16]bool{essdemo.AddrTargetPower: true}
	h := NewHandlerWithAccess(1, regs, writable)

	if _, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId: 1, Addr: essdemo.AddrTargetPower, Quantity: 1, IsWrite: true, Args: []uint16{120},
	}); err != nil {
		t.Fatalf("write target power: %v", err)
	}
	if h.Snapshot()[essdemo.AddrTargetPower] != 120 {
		t.Fatal("target power not updated")
	}

	if _, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId: 1, Addr: essdemo.AddrStatus, Quantity: 1, IsWrite: true, Args: []uint16{9},
	}); err != modbus.ErrIllegalDataAddress {
		t.Fatalf("want illegal address for read-only point, got %v", err)
	}
	if h.Snapshot()[essdemo.AddrStatus] != 2 {
		t.Fatal("read-only register must not change")
	}

	if _, err := h.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{
		UnitId: 1, Addr: 0x1A0, Quantity: 1,
	}); err != modbus.ErrIllegalDataAddress {
		t.Fatalf("want illegal address for undeclared point, got %v", err)
	}
}

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

package slave

import (
	"sync"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/simonvetter/modbus"
)

// Handler serves ess_demo holding registers over Modbus TCP.
type Handler struct {
	mu     sync.RWMutex
	unitID uint8
	regs   map[uint16]uint16
}

func NewHandler(unitID uint8, initial map[uint16]uint16) *Handler {
	regs := make(map[uint16]uint16, len(initial))
	for addr, value := range initial {
		regs[addr] = value
	}
	return &Handler{unitID: unitID, regs: regs}
}

func (h *Handler) acceptUnit(unitID uint8) bool {
	if h.unitID == 0 || unitID == 0 {
		return true
	}
	return unitID == h.unitID
}

func (h *Handler) HandleHoldingRegisters(req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if !h.acceptUnit(req.UnitId) {
		return nil, modbus.ErrIllegalDataAddress
	}
	if req.Quantity == 0 {
		return nil, modbus.ErrIllegalDataAddress
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]uint16, req.Quantity)
	for i := uint16(0); i < req.Quantity; i++ {
		addr := req.Addr + i
		if !essdemo.InRange(addr) {
			return nil, modbus.ErrIllegalDataAddress
		}
		if req.IsWrite {
			if i >= uint16(len(req.Args)) {
				return nil, modbus.ErrIllegalDataValue
			}
			h.regs[addr] = req.Args[i]
		}
		out[i] = h.regs[addr]
	}
	return out, nil
}

func (h *Handler) HandleCoils(req *modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}

func (h *Handler) HandleDiscreteInputs(req *modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalFunction
}

func (h *Handler) HandleInputRegisters(req *modbus.InputRegistersRequest) ([]uint16, error) {
	return nil, modbus.ErrIllegalFunction
}

func (h *Handler) Get(addr uint16) uint16 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.regs[addr]
}

func (h *Handler) Set(addr uint16, value uint16) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.regs[addr] = value
}

func (h *Handler) Snapshot() map[uint16]uint16 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make(map[uint16]uint16, len(h.regs))
	for addr, value := range h.regs {
		out[addr] = value
	}
	return out
}

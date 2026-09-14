package essdemo

// Holding-register map used by OpenLEMS ess_demo_v1.0.0.
// Addresses are PDU addresses (0xC8-0xD5). Scale 0.1 unless noted.
const (
	BaseAddr uint16 = 0xC8
	Count    uint16 = 14

	AddrStatus            uint16 = 0xC8
	AddrPower             uint16 = 0xC9
	AddrSOC               uint16 = 0xCA
	AddrGeneratedEnergy   uint16 = 0xCB
	AddrConsumedEnergy    uint16 = 0xCC
	AddrMaxChargePower    uint16 = 0xCD
	AddrMaxDischargePower uint16 = 0xCE
	AddrDeviceControl     uint16 = 0xCF
	AddrTargetPower       uint16 = 0xD0
	AddrPowerCapacity     uint16 = 0xD1
	AddrEnergyCapacity    uint16 = 0xD2
	AddrMinSOC            uint16 = 0xD3
	AddrMaxSOC            uint16 = 0xD4
	AddrChargeEfficiency  uint16 = 0xD5
)

// DefaultRegisters is a static snapshot that looks like a charging cabinet.
func DefaultRegisters() map[uint16]uint16 {
	return DefaultValues().Registers()
}

func InRange(addr uint16) bool {
	return addr >= BaseAddr && addr < BaseAddr+Count
}

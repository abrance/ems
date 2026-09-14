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
	return map[uint16]uint16{
		AddrStatus:            2,    // charging
		AddrPower:             50,   // 5.0 kW
		AddrSOC:               800,  // 80.0%
		AddrGeneratedEnergy:   1200, // 120.0 kWh
		AddrConsumedEnergy:    1500, // 150.0 kWh
		AddrMaxChargePower:    500,  // 50.0 kW
		AddrMaxDischargePower: 500,  // 50.0 kW
		AddrDeviceControl:     1,    // on
		AddrTargetPower:       50,   // 5.0 kW
		AddrPowerCapacity:     1000, // 100.0 kW
		AddrEnergyCapacity:    2000, // 200.0 kWh
		AddrMinSOC:            100,  // 10.0%
		AddrMaxSOC:            950,  // 95.0%
		AddrChargeEfficiency:  9500, // 95.00 (scale 0.01)
	}
}

func InRange(addr uint16) bool {
	return addr >= BaseAddr && addr < BaseAddr+Count
}

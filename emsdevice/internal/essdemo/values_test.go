package essdemo

import "testing"

func TestDefaultRegistersMatchLegacySnapshot(t *testing.T) {
	got := DefaultRegisters()
	want := map[uint16]uint16{
		AddrStatus:            2,
		AddrPower:             50,
		AddrSOC:               800,
		AddrGeneratedEnergy:   1200,
		AddrConsumedEnergy:    1500,
		AddrMaxChargePower:    500,
		AddrMaxDischargePower: 500,
		AddrDeviceControl:     1,
		AddrTargetPower:       50,
		AddrPowerCapacity:     1000,
		AddrEnergyCapacity:    2000,
		AddrMinSOC:            100,
		AddrMaxSOC:            950,
		AddrChargeEfficiency:  9500,
	}
	for addr, v := range want {
		if got[addr] != v {
			t.Fatalf("addr 0x%X got %d want %d", addr, got[addr], v)
		}
	}
}

func TestEncodeNegativePower(t *testing.T) {
	reg := Encode01(-12.5)
	if Decode01(reg) != -12.5 {
		t.Fatalf("got %v", Decode01(reg))
	}
}

func TestValuesOverlaySOC(t *testing.T) {
	v := DefaultValues()
	v.SOC = 42.3
	if v.Registers()[AddrSOC] != 423 {
		t.Fatalf("soc reg=%d", v.Registers()[AddrSOC])
	}
}

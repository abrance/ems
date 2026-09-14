package essdemo

import "math"

// Values is the ess_demo map in engineering units (kW, kWh, percent).
type Values struct {
	Status            int
	Power             float64
	SOC               float64
	GeneratedEnergy   float64
	ConsumedEnergy    float64
	MaxChargePower    float64
	MaxDischargePower float64
	DeviceControl     int
	TargetPower       float64
	PowerCapacity     float64
	EnergyCapacity    float64
	MinSOC            float64
	MaxSOC            float64
	ChargeEfficiency  float64
}

func DefaultValues() Values {
	return Values{
		Status:            2,
		Power:             5,
		SOC:               80,
		GeneratedEnergy:   120,
		ConsumedEnergy:    150,
		MaxChargePower:    50,
		MaxDischargePower: 50,
		DeviceControl:     1,
		TargetPower:       5,
		PowerCapacity:     100,
		EnergyCapacity:    200,
		MinSOC:            10,
		MaxSOC:            95,
		ChargeEfficiency:  95,
	}
}

func (v Values) Registers() map[uint16]uint16 {
	return map[uint16]uint16{
		AddrStatus:            uint16(v.Status),
		AddrPower:             Encode01(v.Power),
		AddrSOC:               Encode01(v.SOC),
		AddrGeneratedEnergy:   Encode01(v.GeneratedEnergy),
		AddrConsumedEnergy:    Encode01(v.ConsumedEnergy),
		AddrMaxChargePower:    Encode01(v.MaxChargePower),
		AddrMaxDischargePower: Encode01(v.MaxDischargePower),
		AddrDeviceControl:     uint16(v.DeviceControl),
		AddrTargetPower:       Encode01(v.TargetPower),
		AddrPowerCapacity:     Encode01(v.PowerCapacity),
		AddrEnergyCapacity:    Encode01(v.EnergyCapacity),
		AddrMinSOC:            Encode01(v.MinSOC),
		AddrMaxSOC:            Encode01(v.MaxSOC),
		AddrChargeEfficiency:  Encode001(v.ChargeEfficiency),
	}
}

func Encode01(v float64) uint16 {
	return toInt16Reg(v * 10)
}

func Decode01(reg uint16) float64 {
	return float64(int16(reg)) / 10
}

func Encode001(v float64) uint16 {
	return toInt16Reg(v * 100)
}

func toInt16Reg(v float64) uint16 {
	n := int64(math.Round(v))
	if n > math.MaxInt16 {
		n = math.MaxInt16
	}
	if n < math.MinInt16 {
		n = math.MinInt16
	}
	return uint16(int16(n))
}

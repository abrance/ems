package mock

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/abrance/ems/emsdevice/pkg/modbus/slave"
)

type Settings struct {
	Interval    time.Duration
	SOCJitter   float64
	PowerJitter float64
}

func Run(ctx context.Context, h *slave.Handler, s Settings) {
	if s.Interval <= 0 {
		s.Interval = time.Second
	}
	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().Unix())))
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Tick(h, s, rng, s.Interval)
		}
	}
}

func Tick(h *slave.Handler, s Settings, rng *rand.Rand, dt time.Duration) {
	if h.Get(essdemo.AddrDeviceControl) == 0 {
		h.Set(essdemo.AddrStatus, 0)
		h.Set(essdemo.AddrPower, essdemo.Encode01(0))
		return
	}

	target := essdemo.Decode01(h.Get(essdemo.AddrTargetPower))
	maxCharge := essdemo.Decode01(h.Get(essdemo.AddrMaxChargePower))
	maxDischarge := essdemo.Decode01(h.Get(essdemo.AddrMaxDischargePower))
	minSOC := essdemo.Decode01(h.Get(essdemo.AddrMinSOC))
	maxSOC := essdemo.Decode01(h.Get(essdemo.AddrMaxSOC))
	capacity := essdemo.Decode01(h.Get(essdemo.AddrEnergyCapacity))
	soc := essdemo.Decode01(h.Get(essdemo.AddrSOC))
	generated := essdemo.Decode01(h.Get(essdemo.AddrGeneratedEnergy))
	consumed := essdemo.Decode01(h.Get(essdemo.AddrConsumedEnergy))

	power := target + jitter(rng, s.PowerJitter)
	if power > maxCharge {
		power = maxCharge
	}
	if power < -maxDischarge {
		power = -maxDischarge
	}

	hours := dt.Seconds() / 3600
	if capacity > 0 {
		soc += (power / capacity) * hours * 100
	}
	soc += jitter(rng, s.SOCJitter)
	if soc < minSOC {
		soc = minSOC
		if power < 0 {
			power = 0
		}
	}
	if soc > maxSOC {
		soc = maxSOC
		if power > 0 {
			power = 0
		}
	}

	energyDelta := math.Abs(power) * hours
	if power >= 0 {
		consumed += energyDelta
		h.Set(essdemo.AddrStatus, 2)
	} else {
		generated += energyDelta
		h.Set(essdemo.AddrStatus, 3)
	}
	if math.Abs(power) < 0.05 {
		h.Set(essdemo.AddrStatus, 1)
	}

	h.Set(essdemo.AddrPower, essdemo.Encode01(power))
	h.Set(essdemo.AddrSOC, essdemo.Encode01(soc))
	h.Set(essdemo.AddrConsumedEnergy, essdemo.Encode01(consumed))
	h.Set(essdemo.AddrGeneratedEnergy, essdemo.Encode01(generated))
}

func jitter(rng *rand.Rand, amplitude float64) float64 {
	if amplitude <= 0 || rng == nil {
		return 0
	}
	return (rng.Float64()*2 - 1) * amplitude
}

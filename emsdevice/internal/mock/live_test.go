package mock

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/abrance/ems/emsdevice/internal/devicetable"
	"github.com/abrance/ems/emsdevice/internal/essdemo"
	"github.com/abrance/ems/emsdevice/pkg/modbus/slave"
)

func TestTickStopsWhenControlOff(t *testing.T) {
	h := slave.NewHandler(1, essdemo.DefaultRegisters())
	h.Set(essdemo.AddrDeviceControl, 0)
	Tick(h, Settings{PowerJitter: 10, SOCJitter: 10}, rand.New(rand.NewPCG(1, 2)), time.Second)
	if h.Get(essdemo.AddrStatus) != 0 {
		t.Fatalf("status=%d", h.Get(essdemo.AddrStatus))
	}
	if essdemo.Decode01(h.Get(essdemo.AddrPower)) != 0 {
		t.Fatalf("power=%v", essdemo.Decode01(h.Get(essdemo.AddrPower)))
	}
}

func TestTickFollowsTargetPower(t *testing.T) {
	h := slave.NewHandler(1, essdemo.DefaultRegisters())
	h.Set(essdemo.AddrTargetPower, essdemo.Encode01(20))
	Tick(h, Settings{}, nil, time.Hour)
	if got := essdemo.Decode01(h.Get(essdemo.AddrPower)); got != 20 {
		t.Fatalf("power=%v", got)
	}
	if h.Get(essdemo.AddrStatus) != 2 {
		t.Fatalf("status=%d", h.Get(essdemo.AddrStatus))
	}
	soc := essdemo.Decode01(h.Get(essdemo.AddrSOC))
	if soc <= 80 {
		t.Fatalf("soc should rise while charging, got %v", soc)
	}
}

// TestJitterExtraStaysAroundBase pins the fix for the unanchored random walk:
// every tick must stay within jitter of the configured value, and the value
// must actually move.
func TestJitterExtraStaysAroundBase(t *testing.T) {
	base := devicetable.Point{
		Addr: 0x65, Key: "Power", Signed: true, Scale: 0.1,
		Value: 60, Jitter: 3, HasRange: true, Min: 0, Max: 120,
	}
	h := slave.NewHandler(1, map[uint16]uint16{base.Addr: base.Encode(base.Value)})
	s := Settings{Jitter: []devicetable.Point{base}}
	rng := rand.New(rand.NewPCG(7, 8))

	minSeen, maxSeen := math.Inf(1), math.Inf(-1)
	for i := 0; i < 5000; i++ {
		Tick(h, s, rng, time.Second)
		v := base.Decode(h.Get(base.Addr))
		if math.Abs(v-base.Value) > base.Jitter+1e-9 {
			t.Fatalf("tick %d: %v drifted away from base %v (jitter %v)", i, v, base.Value, base.Jitter)
		}
		minSeen = math.Min(minSeen, v)
		maxSeen = math.Max(maxSeen, v)
	}
	if minSeen >= base.Value || maxSeen <= base.Value {
		t.Fatalf("jitter never moved: min=%v max=%v", minSeen, maxSeen)
	}
}

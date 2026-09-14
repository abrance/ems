package mock

import (
	"math/rand/v2"
	"testing"
	"time"

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

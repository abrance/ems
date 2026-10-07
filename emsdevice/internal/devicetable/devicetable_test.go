package devicetable

import (
	"math"
	"testing"
)

func TestEncodeDecode(t *testing.T) {
	scaled := Point{Scale: 0.1, Signed: true}
	if got := scaled.Decode(scaled.Encode(-12.3)); got != -12.3 {
		t.Fatalf("signed scale 0.1: got %v", got)
	}
	unsigned := Point{Scale: 0.1, Signed: false}
	if got := unsigned.Decode(unsigned.Encode(1200)); got != 1200 {
		t.Fatalf("unsigned scale 0.1: got %v", got)
	}
	if got := unsigned.Encode(-5); got != 0 {
		t.Fatalf("unsigned must clamp negatives, got 0x%X", got)
	}
	small := Point{Scale: 0.0001, Signed: true}
	if got := small.Decode(small.Encode(0.96)); math.Abs(got-0.96) > 1e-9 {
		t.Fatalf("scale 0.0001: got %v", got)
	}
}

func TestRegistersAndWritable(t *testing.T) {
	devices := []Device{
		{Key: "ess", Points: []Point{
			{Addr: 0xC9, Key: "Power", Scale: 0.1, Value: 5, Signed: true},
			{Addr: 0xD0, Key: "TargetPower", Scale: 0.1, Value: 5, Signed: true, Writable: true},
		}},
		{Key: "ammeter", Points: []Point{
			{Addr: 0x1A0, Key: "TotalActivePower", Scale: 0.1, Value: 25, Signed: true},
		}},
	}
	regs := Registers(devices)
	if regs[0xC9] != 50 || regs[0xD0] != 50 || regs[0x1A0] != 250 {
		t.Fatalf("registers %v", regs)
	}
	writable := Writable(devices)
	if len(writable) != 1 || !writable[0xD0] {
		t.Fatalf("writable %v", writable)
	}
	if writable[0xC9] {
		t.Fatal("read-only point must not be writable")
	}
}

func TestValidateRejects(t *testing.T) {
	if err := Validate(nil); err != nil {
		t.Fatalf("empty table is allowed (legacy mode): %v", err)
	}
	dup := []Device{
		{Key: "a", Points: []Point{{Addr: 0xC8, Key: "X", Scale: 1}}},
		{Key: "b", Points: []Point{{Addr: 0xC8, Key: "Y", Scale: 1}}},
	}
	if err := Validate(dup); err == nil {
		t.Fatal("duplicate address must fail")
	}
	bad := []Device{{Key: "a", Points: []Point{{Addr: 0xC8, Key: "X", Scale: 0}}}}
	if err := Validate(bad); err == nil {
		t.Fatal("zero scale must fail")
	}
	badRange := []Device{{Key: "a", Points: []Point{{Addr: 0xC8, Key: "X", Scale: 1, HasRange: true, Min: 10, Max: 1}}}}
	if err := Validate(badRange); err == nil {
		t.Fatal("min > max must fail")
	}
}

func TestJitterPoints(t *testing.T) {
	devices := []Device{{Key: "pv", Points: []Point{
		{Addr: 0x65, Key: "Power", Scale: 0.1, Jitter: 3},
		{Addr: 0x66, Key: "GeneratedEnergy", Scale: 0.1},
	}}}
	points := JitterPoints(devices)
	if len(points) != 1 || points[0].Addr != 0x65 {
		t.Fatalf("jitter points %v", points)
	}
}

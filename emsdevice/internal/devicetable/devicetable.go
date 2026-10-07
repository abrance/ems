// Package devicetable holds the simulated Modbus point tables served by
// emsdevice. Each table mirrors a demo driver in OpenLEMS (ess_demo, pv_demo,
// load_demo, ammeter_demo) so the EMS side reads the same addresses it would
// read from a real device. Tables are declared in emsdevice.json and can be
// edited without rebuilding the binary.
package devicetable

import (
	"fmt"
	"math"
)

// Point is one holding register in a device point table.
type Point struct {
	Addr     uint16
	Key      string
	Name     string
	Signed   bool    // int16 when true, uint16 otherwise
	Scale    float64 // engineering value = raw * Scale
	Writable bool    // FC06/FC16 accepted on this address
	Value    float64 // engineering value at startup
	Jitter   float64 // live-mode random walk amplitude (engineering units); 0 = static
	HasRange bool    // clamp jittered value to [Min, Max]
	Min      float64
	Max      float64
}

// Device groups a point table. Key/Name are for readability only.
type Device struct {
	Key    string
	Name   string
	Points []Point
}

// Encode converts an engineering value to the raw register value, clamped to
// the point's integer range.
func (p Point) Encode(v float64) uint16 {
	scale := p.Scale
	if scale == 0 {
		scale = 1
	}
	n := math.Round(v / scale)
	if p.Signed {
		if n > math.MaxInt16 {
			n = math.MaxInt16
		}
		if n < math.MinInt16 {
			n = math.MinInt16
		}
		return uint16(int16(n))
	}
	if n < 0 {
		n = 0
	}
	if n > math.MaxUint16 {
		n = math.MaxUint16
	}
	return uint16(n)
}

// Decode converts a raw register value to engineering units.
func (p Point) Decode(raw uint16) float64 {
	scale := p.Scale
	if scale == 0 {
		scale = 1
	}
	if p.Signed {
		return float64(int16(raw)) * scale
	}
	return float64(raw) * scale
}

// Validate rejects malformed or overlapping tables.
func Validate(devices []Device) error {
	seen := make(map[uint16]string)
	for _, d := range devices {
		if d.Key == "" {
			return fmt.Errorf("device key must not be empty")
		}
		if len(d.Points) == 0 {
			return fmt.Errorf("device %q has no points", d.Key)
		}
		for _, p := range d.Points {
			label := fmt.Sprintf("%s.%s(0x%X)", d.Key, p.Key, p.Addr)
			if p.Scale == 0 {
				return fmt.Errorf("%s: scale must not be 0", label)
			}
			if p.Scale < 0 {
				return fmt.Errorf("%s: scale must be > 0", label)
			}
			if p.Jitter < 0 {
				return fmt.Errorf("%s: jitter must be >= 0", label)
			}
			if p.HasRange && p.Min > p.Max {
				return fmt.Errorf("%s: min %v > max %v", label, p.Min, p.Max)
			}
			if prev, ok := seen[p.Addr]; ok {
				return fmt.Errorf("address 0x%X declared twice: %s and %s", p.Addr, prev, label)
			}
			seen[p.Addr] = label
		}
	}
	return nil
}

// Registers seeds the register map from the point tables.
func Registers(devices []Device) map[uint16]uint16 {
	out := make(map[uint16]uint16)
	for _, d := range devices {
		for _, p := range d.Points {
			out[p.Addr] = p.Encode(p.Value)
		}
	}
	return out
}

// Writable returns the write whitelist. The result is never nil: a table with
// no writable point rejects every write.
func Writable(devices []Device) map[uint16]bool {
	out := make(map[uint16]bool)
	for _, d := range devices {
		for _, p := range d.Points {
			if p.Writable {
				out[p.Addr] = true
			}
		}
	}
	return out
}

// JitterPoints returns the points that move in live mode.
func JitterPoints(devices []Device) []Point {
	var out []Point
	for _, d := range devices {
		for _, p := range d.Points {
			if p.Jitter > 0 {
				out = append(out, p)
			}
		}
	}
	return out
}

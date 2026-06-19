package adaptive

import (
	"encoding/base64"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/tlv8"
)

func newBrightness() *characteristic.Brightness { return characteristic.NewBrightness() }
func newColorTemperature() *characteristic.ColorTemperature {
	return characteristic.NewColorTemperature()
}

func TestNewControllerAddsCharacteristics(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)

	c := NewController(Options{
		Lightbulb:           a.Lightbulb,
		Brightness:          bright,
		ColorTemperature:    ct,
		SetColorTemperature: func(int) error { return nil },
	})
	if c == nil {
		t.Fatal("nil controller")
	}
	if a.Lightbulb.C(characteristic.TypeSupportedCharacteristicValueTransitionConfiguration) == nil {
		t.Fatal("supported transition characteristic not added")
	}
	if a.Lightbulb.C(characteristic.TypeCharacteristicValueTransitionControl) == nil {
		t.Fatal("transition control characteristic not added")
	}
	if a.Lightbulb.C(characteristic.TypeCharacteristicValueActiveTransitionCount) == nil {
		t.Fatal("active transition count characteristic not added")
	}
}

func TestSupportedConfigReadListsIIDs(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	bright.C.Id = 3
	ct.C.Id = 7

	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(int) error { return nil },
	})

	val, code := c.supported.ValueRequestFunc(nil)
	if code != 0 {
		t.Fatalf("read code = %d, want 0", code)
	}
	s, ok := val.(string)
	if !ok || s == "" {
		t.Fatalf("expected non-empty base64 string, got %T %q", val, val)
	}
}

func enablePayload(ctIID, brightIID uint64, startMillis int64) string {
	since := uint64(startMillis - hapEpoch.UnixMilli())
	var start [8]byte
	for i := 0; i < 8; i++ {
		start[i] = byte(since >> (8 * i))
	}
	w := controlWrite{Update: &updateRequest{Config: valueTransitionConfig{
		IID:     ctIID,
		Enabled: 1,
		Parameters: transitionParameters{
			TransitionID: make([]byte, 16),
			StartTime:    start[:],
		},
		Curve: curveConfig{
			Entries: []curveEntryTLV{
				{AdjustmentFactor: 0, Temperature: 200, TransitionOffset: 0},
				{AdjustmentFactor: 0, Temperature: 300, TransitionOffset: 600000},
			},
			AdjustmentIID:   brightIID,
			MultiplierRange: multiplierRange{Min: 10, Max: 100},
		},
		UpdateInterval:          60000,
		NotifyIntervalThreshold: 600000,
	}}}
	b, _ := tlv8.Marshal(w)
	return base64.StdEncoding.EncodeToString(b)
}

func TestControlWriteEnables(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	ct.C.Id, bright.C.Id = 7, 3
	bright.SetValue(100)

	var commanded int64
	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(m int) error { atomic.StoreInt64(&commanded, int64(m)); return nil },
		Now:                 func() time.Time { return hapEpoch.Add(5 * time.Minute) },
	})

	resp, code := c.handleControlWrite(enablePayload(7, 3, hapEpoch.UnixMilli()), (*http.Request)(nil))
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if resp == nil {
		t.Fatal("expected a write-response body")
	}
	if !c.IsActive() {
		t.Fatal("expected AL active after enable")
	}
	if c.count.Value() != 1 {
		t.Fatalf("active count = %d, want 1", c.count.Value())
	}
	// With a constant injected Now, the clock-skew correction cancels: the
	// schedule's TimeOffsetMillis = now-startMillis, so the computed offset at
	// the instant of enable is 0 -> evaluate at curve position 0 -> 200 mired.
	if got := atomic.LoadInt64(&commanded); got != 200 {
		t.Fatalf("commanded mired = %d, want 200", got)
	}
	c.Disable() // stop the background timer so the test process is clean
}

func TestControlWriteDisables(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	ct.C.Id, bright.C.Id = 7, 3
	bright.SetValue(100)

	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(int) error { return nil },
		Now:                 func() time.Time { return hapEpoch.Add(time.Minute) },
	})
	c.handleControlWrite(enablePayload(7, 3, hapEpoch.UnixMilli()), (*http.Request)(nil))
	if !c.IsActive() {
		t.Fatal("expected active after enable")
	}

	// iid-only update = disable.
	w := controlWrite{Update: &updateRequest{Config: valueTransitionConfig{IID: 7}}}
	b, _ := tlv8.Marshal(w)
	c.handleControlWrite(base64.StdEncoding.EncodeToString(b), (*http.Request)(nil))
	if c.IsActive() {
		t.Fatal("expected AL inactive after disable")
	}
	if c.count.Value() != 0 {
		t.Fatalf("active count = %d, want 0", c.count.Value())
	}
}

func TestBrightnessChangeRecomputes(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	ct.C.Id, bright.C.Id = 7, 3
	bright.SetValue(100)

	var commanded int64
	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(m int) error { atomic.StoreInt64(&commanded, int64(m)); return nil },
		Now:                 func() time.Time { return hapEpoch.Add(5 * time.Minute) },
	})

	// Flat temperature 300 with adjustment factor -1 -> mired = 300 - clamp(brightness).
	w := controlWrite{Update: &updateRequest{Config: valueTransitionConfig{
		IID: 7, Enabled: 1,
		Parameters: transitionParameters{TransitionID: make([]byte, 16), StartTime: make([]byte, 8)},
		Curve: curveConfig{
			Entries: []curveEntryTLV{
				{AdjustmentFactor: -1, Temperature: 300, TransitionOffset: 0},
				{AdjustmentFactor: -1, Temperature: 300, TransitionOffset: 600000},
			},
			AdjustmentIID: 3, MultiplierRange: multiplierRange{Min: 10, Max: 100},
		},
		UpdateInterval: 60000, NotifyIntervalThreshold: 600000,
	}}}
	b, _ := tlv8.Marshal(w)
	c.handleControlWrite(base64.StdEncoding.EncodeToString(b), (*http.Request)(nil))
	if got := atomic.LoadInt64(&commanded); got != 200 { // 300 - 100
		t.Fatalf("after enable commanded = %d, want 200", got)
	}

	bright.SetValue(50)
	c.HandleBrightnessChanged()
	if got := atomic.LoadInt64(&commanded); got != 250 { // 300 - 50
		t.Fatalf("after dim commanded = %d, want 250", got)
	}

	c.Disable() // stop the background timer
}

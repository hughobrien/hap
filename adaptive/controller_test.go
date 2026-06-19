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

func TestSerializeRestore(t *testing.T) {
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
		Now:                 func() time.Time { return hapEpoch.Add(5 * time.Minute) },
	})
	c.handleControlWrite(enablePayload(7, 3, hapEpoch.UnixMilli()), (*http.Request)(nil))

	blob, ok := c.Serialize()
	if !ok {
		t.Fatal("expected serialized state when active")
	}
	c.Disable()

	// Fresh controller, restore.
	a2 := accessory.NewLightbulb(accessory.Info{Name: "Test2"})
	bright2 := newBrightness()
	ct2 := newColorTemperature()
	a2.Lightbulb.AddC(bright2.C)
	a2.Lightbulb.AddC(ct2.C)
	ct2.C.Id, bright2.C.Id = 7, 3
	bright2.SetValue(100)

	var commanded int64
	c2 := NewController(Options{
		Lightbulb: a2.Lightbulb, Brightness: bright2, ColorTemperature: ct2,
		SetColorTemperature: func(m int) error { atomic.StoreInt64(&commanded, int64(m)); return nil },
		Now:                 func() time.Time { return hapEpoch.Add(5 * time.Minute) },
	})
	if err := c2.Restore(blob); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !c2.IsActive() {
		t.Fatal("expected active after restore")
	}
	// At the same Now used at enable, offset = 0 -> curve start 200 mired (flat ramp 200->300, brightness factor 0).
	if got := atomic.LoadInt64(&commanded); got != 200 {
		t.Fatalf("restored commanded = %d, want 200", got)
	}
	c2.Disable()
}

func TestSerializeWhenInactive(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(int) error { return nil },
	})
	if _, ok := c.Serialize(); ok {
		t.Fatal("expected ok=false when inactive")
	}
	if err := c.Restore(nil); err != nil {
		t.Fatalf("restore(nil) should be a no-op, got %v", err)
	}
	if c.IsActive() {
		t.Fatal("restore(nil) must not activate")
	}
}

func TestControlReadValueWhenActive(t *testing.T) {
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

	b, err := c.buildControlReadValue()
	if err != nil {
		t.Fatalf("read value: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty read value when active")
	}
	// It must be decodable back into the read-response shape and preserve the curve.
	var rr readResponse
	if err := tlv8.Unmarshal(b, &rr); err != nil {
		t.Fatalf("decode read response: %v", err)
	}
	if rr.Config.IID != 7 {
		t.Fatalf("iid = %d, want 7", rr.Config.IID)
	}
	if len(rr.Config.Curve.Entries) != 2 {
		t.Fatalf("curve entries = %d, want 2", len(rr.Config.Curve.Entries))
	}
	if rr.Config.Curve.Entries[1].Temperature != 300 {
		t.Fatalf("entry[1] temp = %v, want 300 (float32 encode must work)", rr.Config.Curve.Entries[1].Temperature)
	}
	c.Disable()
}

func TestControlReadValueWhenInactive(t *testing.T) {
	a := accessory.NewLightbulb(accessory.Info{Name: "Test"})
	bright := newBrightness()
	ct := newColorTemperature()
	a.Lightbulb.AddC(bright.C)
	a.Lightbulb.AddC(ct.C)
	c := NewController(Options{
		Lightbulb: a.Lightbulb, Brightness: bright, ColorTemperature: ct,
		SetColorTemperature: func(int) error { return nil },
	})
	b, err := c.buildControlReadValue()
	if err != nil || len(b) != 0 {
		t.Fatalf("expected empty read value when inactive, got %d bytes err=%v", len(b), err)
	}
}

package adaptive

import (
	"testing"

	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
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

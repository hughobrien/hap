package adaptive

import "testing"

func sampleCurve() []curvePoint {
	// Two points: at t=0 temp=200, at t=600000ms (10 min) temp=300.
	// brightnessAdjustmentFactor 0 to keep the test about time interpolation.
	return []curvePoint{
		{Temperature: 200, AdjustmentFactor: 0, TransitionTime: 0, Duration: 0},
		{Temperature: 300, AdjustmentFactor: 0, TransitionTime: 600000, Duration: 0},
	}
}

func TestEvaluateMidpoint(t *testing.T) {
	c := sampleCurve()
	// Halfway through the 10-minute segment -> 250 mired.
	temp, ok := evaluate(c, 300000, 100, brightnessRange{Min: 10, Max: 100})
	if !ok {
		t.Fatal("expected a value within the curve")
	}
	if temp != 250 {
		t.Fatalf("temp = %d, want 250", temp)
	}
}

func TestEvaluateBrightnessAdjustment(t *testing.T) {
	c := []curvePoint{
		{Temperature: 200, AdjustmentFactor: -1, TransitionTime: 0},
		{Temperature: 200, AdjustmentFactor: -1, TransitionTime: 600000},
	}
	// temp 200 + (-1 * clamp(50)) = 150.
	temp, ok := evaluate(c, 300000, 50, brightnessRange{Min: 10, Max: 100})
	if !ok || temp != 150 {
		t.Fatalf("temp = %d ok=%v, want 150 true", temp, ok)
	}
}

func TestEvaluatePastEndReturnsFalse(t *testing.T) {
	c := sampleCurve()
	if _, ok := evaluate(c, 999999999, 100, brightnessRange{Min: 10, Max: 100}); ok {
		t.Fatal("expected ok=false past the end of the curve")
	}
}

func TestEvaluateClampsBrightness(t *testing.T) {
	c := []curvePoint{
		{Temperature: 200, AdjustmentFactor: -1, TransitionTime: 0},
		{Temperature: 200, AdjustmentFactor: -1, TransitionTime: 600000},
	}
	// brightness 5 is below min 10 -> clamps to 10 -> 200 - 10 = 190.
	temp, _ := evaluate(c, 300000, 5, brightnessRange{Min: 10, Max: 100})
	if temp != 190 {
		t.Fatalf("temp = %d, want 190", temp)
	}
}

package adaptive

import (
	"testing"

	"github.com/brutella/hap/tlv8"
)

func TestDecodeUpdateRoundTrip(t *testing.T) {
	in := controlWrite{
		Update: &updateRequest{
			Config: valueTransitionConfig{
				IID:     7,
				Enabled: 1,
				Parameters: transitionParameters{
					TransitionID: make([]byte, 16),
					StartTime:    []byte{0, 0, 0, 0, 0, 0, 0, 0},
				},
				Curve: curveConfig{
					Entries: []curveEntryTLV{
						{AdjustmentFactor: -1.5, Temperature: 200, TransitionOffset: 0},
						{AdjustmentFactor: -2.0, Temperature: 300, TransitionOffset: 1800000},
					},
					AdjustmentIID:   3,
					MultiplierRange: multiplierRange{Min: 10, Max: 100},
				},
				UpdateInterval:          60000,
				NotifyIntervalThreshold: 600000,
			},
		},
	}

	b, err := tlv8.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Logf("encoded: %x", b)

	var out controlWrite
	if err := tlv8.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Update == nil {
		t.Fatal("update missing")
	}
	if got := len(out.Update.Config.Curve.Entries); got != 2 {
		t.Fatalf("curve entries = %d, want 2", got)
	}
	if out.Update.Config.Curve.Entries[1].Temperature != 300 {
		t.Fatalf("entry[1] temp = %v, want 300", out.Update.Config.Curve.Entries[1].Temperature)
	}
	if out.Update.Config.IID != 7 {
		t.Fatalf("iid = %d, want 7", out.Update.Config.IID)
	}
}

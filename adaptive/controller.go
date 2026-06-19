package adaptive

import (
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/service"
	"github.com/brutella/hap/tlv8"
)

// Options configures a Controller.
type Options struct {
	// Lightbulb is the service the transition characteristics are added to.
	Lightbulb *service.Lightbulb
	// Brightness and ColorTemperature must already be added to Lightbulb.
	Brightness       *characteristic.Brightness
	ColorTemperature *characteristic.ColorTemperature
	// SetColorTemperature drives the physical device. Called on every tick and
	// on brightness changes while AL is active. Must be safe to call from a
	// goroutine.
	SetColorTemperature func(mired int) error
	// Now returns the current time; defaults to time.Now. Injectable for tests.
	Now func() time.Time
	// OnStateChange (optional) is called after the active transition changes
	// (enable, disable, renew). The host re-persists Serialize() output.
	OnStateChange func()
}

// Controller implements Adaptive Lighting for one Lightbulb.
type Controller struct {
	opts Options
	now  func() time.Time

	supported *characteristic.SupportedCharacteristicValueTransitionConfiguration
	control   *characteristic.CharacteristicValueTransitionControl
	count     *characteristic.CharacteristicValueActiveTransitionCount

	mu     sync.Mutex
	active *activeTransition
	timer  *time.Timer
}

// activeTransition is the running schedule (also the serialized form).
type activeTransition struct {
	IID              uint64
	BrightnessIID    uint64
	TransitionID     []byte
	StartTimeBuf     []byte
	Unknown3         []byte
	StartMillis      int64
	TimeOffsetMillis int64 // localNow - startMillis at setup, absorbs clock skew
	Curve            []curvePoint
	Range            brightnessRange
	UpdateInterval   time.Duration
	NotifyThreshold  time.Duration
}

func NewController(opts Options) *Controller {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &Controller{
		opts:      opts,
		now:       opts.Now,
		supported: characteristic.NewSupportedCharacteristicValueTransitionConfiguration(),
		control:   characteristic.NewCharacteristicValueTransitionControl(),
		count:     characteristic.NewCharacteristicValueActiveTransitionCount(),
	}

	opts.Lightbulb.AddC(c.supported.C)
	opts.Lightbulb.AddC(c.control.C)
	opts.Lightbulb.AddC(c.count.C)

	// Supported config is computed lazily so characteristic IIDs are assigned.
	c.supported.ValueRequestFunc = func(*http.Request) (interface{}, int) {
		b, err := c.buildSupportedConfig()
		if err != nil {
			return nil, -70402
		}
		return base64Std(b), 0
	}

	c.control.SetValueRequestFunc = func(v interface{}, r *http.Request) (interface{}, int) {
		return c.handleControlWrite(v, r)
	}
	c.control.ValueRequestFunc = func(*http.Request) (interface{}, int) {
		b, err := c.buildControlReadValue()
		if err != nil {
			return nil, -70402
		}
		return base64Std(b), 0
	}

	return c
}

func (c *Controller) buildSupportedConfig() ([]byte, error) {
	return tlv8.Marshal(supportedConfig{
		Entries: []supportedEntry{
			{IID: c.opts.Brightness.C.Id, TransitionType: transitionTypeBrightness},
			{IID: c.opts.ColorTemperature.C.Id, TransitionType: transitionTypeColorTemperature},
		},
	})
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// --- Stubs replaced in later tasks ---

func (c *Controller) handleControlWrite(v interface{}, r *http.Request) (interface{}, int) {
	return nil, 0
}

func (c *Controller) buildControlReadValue() ([]byte, error) { return []byte{}, nil }

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

// IsActive reports whether an Adaptive Lighting transition is running.
func (c *Controller) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active != nil
}

func (c *Controller) handleControlWrite(v interface{}, r *http.Request) (interface{}, int) {
	str, ok := v.(string)
	if !ok {
		return nil, -70410
	}
	raw, err := base64.StdEncoding.DecodeString(str)
	if err != nil {
		return nil, -70410
	}

	var w controlWrite
	if err := tlv8.Unmarshal(raw, &w); err != nil {
		return nil, -70410
	}

	if w.Update != nil {
		cfg := w.Update.Config
		// An update without the enable flag (iid only) means disable.
		if cfg.Enabled != 1 || len(cfg.Curve.Entries) == 0 {
			c.Disable()
			b, _ := tlv8.Marshal(struct {
				Update []byte `tlv8:"2"`
			}{Update: []byte{}})
			return base64Std(b), 0
		}
		if err := c.enable(cfg); err != nil {
			c.Disable()
			return nil, -70402
		}
		c.mu.Lock()
		params, timeSince := c.statusLocked()
		iid := c.active.IID
		c.mu.Unlock()
		body, _ := buildStatusResponse(iid, params, timeSince)
		return base64Std(body), 0
	}

	if w.Read != nil {
		b, err := c.buildControlReadValue()
		if err != nil {
			return nil, -70402
		}
		return base64Std(b), 0
	}

	return base64Std([]byte{}), 0
}

func (c *Controller) enable(cfg valueTransitionConfig) error {
	curve := make([]curvePoint, len(cfg.Curve.Entries))
	for i, e := range cfg.Curve.Entries {
		curve[i] = curvePoint{
			Temperature:      float64(e.Temperature),
			AdjustmentFactor: float64(e.AdjustmentFactor),
			TransitionTime:   int64(e.TransitionOffset),
			Duration:         int64(e.Duration),
		}
	}
	startMillis := startTimeMillis(cfg.Parameters.StartTime)
	updateInterval := time.Duration(cfg.UpdateInterval) * time.Millisecond
	if updateInterval <= 0 {
		updateInterval = 60 * time.Second
	}

	c.mu.Lock()
	c.active = &activeTransition{
		IID:              cfg.IID,
		BrightnessIID:    cfg.Curve.AdjustmentIID,
		TransitionID:     cfg.Parameters.TransitionID,
		StartTimeBuf:     cfg.Parameters.StartTime,
		Unknown3:         cfg.Parameters.Unknown3,
		StartMillis:      startMillis,
		TimeOffsetMillis: c.now().UnixMilli() - startMillis,
		Curve:            curve,
		Range:            brightnessRange{Min: int(cfg.Curve.MultiplierRange.Min), Max: int(cfg.Curve.MultiplierRange.Max)},
		UpdateInterval:   updateInterval,
		NotifyThreshold:  time.Duration(cfg.NotifyIntervalThreshold) * time.Millisecond,
	}
	c.mu.Unlock()

	c.count.SetValue(1)
	c.tick()
	if c.opts.OnStateChange != nil {
		c.opts.OnStateChange()
	}
	return nil
}

// tick computes and applies the colour temperature for "now" and schedules the
// next update. Safe to call while active; a no-op if AL was disabled.
func (c *Controller) tick() {
	c.mu.Lock()
	if c.active == nil {
		c.mu.Unlock()
		return
	}
	at := c.active
	offset := c.now().UnixMilli() - at.TimeOffsetMillis - at.StartMillis
	brightness := c.opts.Brightness.Value()
	mired, ok := evaluate(at.Curve, offset, brightness, at.Range)
	interval := at.UpdateInterval
	c.mu.Unlock()

	if !ok {
		c.Disable() // curve exhausted
		return
	}

	mired = clampCT(c.opts.ColorTemperature, mired)

	if err := c.opts.SetColorTemperature(mired); err == nil {
		// Reflect on the characteristic for the Home UI without re-triggering a
		// remote-write callback (SetValue uses a nil request).
		c.opts.ColorTemperature.SetValue(mired)
	}

	c.mu.Lock()
	if c.active != nil {
		if c.timer != nil {
			c.timer.Stop()
		}
		c.timer = time.AfterFunc(interval, c.tick)
	}
	c.mu.Unlock()
}

// Disable stops Adaptive Lighting and clears all state.
func (c *Controller) Disable() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	wasActive := c.active != nil
	c.active = nil
	c.mu.Unlock()

	if wasActive {
		c.count.SetValue(0)
		if c.opts.OnStateChange != nil {
			c.opts.OnStateChange()
		}
	}
}

// statusLocked builds the parameters + time-since-start for a response. Caller
// must hold c.mu and c.active must be non-nil.
func (c *Controller) statusLocked() (transitionParameters, uint64) {
	at := c.active
	params := transitionParameters{
		TransitionID: at.TransitionID,
		StartTime:    at.StartTimeBuf,
		Unknown3:     at.Unknown3,
	}
	timeSince := c.now().UnixMilli() - at.TimeOffsetMillis - at.StartMillis
	if timeSince < 0 {
		timeSince = 0
	}
	return params, uint64(timeSince)
}

func clampCT(ct *characteristic.ColorTemperature, v int) int {
	if v < ct.MinValue() {
		return ct.MinValue()
	}
	if v > ct.MaxValue() {
		return ct.MaxValue()
	}
	return v
}

// HandleBrightnessChanged recomputes the colour temperature for the current
// brightness. The host app calls this when brightness changes (warm-on-dim).
// No-op when AL is inactive.
func (c *Controller) HandleBrightnessChanged() {
	if c.IsActive() {
		c.tick()
	}
}

// --- Stubs replaced in later tasks ---

func (c *Controller) buildControlReadValue() ([]byte, error) { return []byte{}, nil }

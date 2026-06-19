// Package adaptive implements HomeKit Adaptive Lighting for Lightbulb
// accessories: the transition characteristics, the TLV8 schedule codec, the
// curve evaluation, and a controller that drives colour temperature over time.
package adaptive

import (
	"encoding/binary"
	"time"

	"github.com/brutella/hap/tlv8"
)

// hapEpoch is 2001-01-01 00:00:00 UTC. HomeKit transition start times are
// expressed in milliseconds since this instant.
var hapEpoch = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

// Transition types in the Supported Transition Configuration.
const (
	transitionTypeBrightness       byte = 0x01
	transitionTypeColorTemperature byte = 0x02
)

// supportedConfig is the value of the Supported Transition Configuration
// characteristic: a list of (iid, transition-type) pairs.
type supportedConfig struct {
	Entries []supportedEntry `tlv8:"1"`
}

type supportedEntry struct {
	IID            uint64 `tlv8:"1"`
	TransitionType byte   `tlv8:"2"`
}

// controlWrite is the top-level payload written to Transition Control.
type controlWrite struct {
	Read   *readRequest   `tlv8:"1,optional"`
	Update *updateRequest `tlv8:"2,optional"`
}

type readRequest struct {
	IID uint64 `tlv8:"1"`
}

type updateRequest struct {
	Config valueTransitionConfig `tlv8:"1"`
}

type valueTransitionConfig struct {
	IID                     uint64               `tlv8:"1"`
	Parameters              transitionParameters `tlv8:"2"`
	Enabled                 byte                 `tlv8:"3,optional"` // 1 = enable; absent = disable
	Curve                   curveConfig          `tlv8:"5,optional"`
	UpdateInterval          uint16               `tlv8:"6,optional"`
	NotifyIntervalThreshold uint32               `tlv8:"8,optional"`
}

type transitionParameters struct {
	TransitionID []byte `tlv8:"1"` // 16 bytes
	StartTime    []byte `tlv8:"2"` // 8 bytes, millis since 2001 LE
	Unknown3     []byte `tlv8:"3,optional"`
}

type curveConfig struct {
	Entries         []curveEntryTLV `tlv8:"1"`
	AdjustmentIID   uint64          `tlv8:"2"`
	MultiplierRange multiplierRange `tlv8:"3"`
}

type curveEntryTLV struct {
	AdjustmentFactor float32 `tlv8:"1"`
	Temperature      float32 `tlv8:"2"`
	TransitionOffset uint32  `tlv8:"3"`
	Duration         uint32  `tlv8:"4,optional"`
}

type multiplierRange struct {
	Min uint32 `tlv8:"1"`
	Max uint32 `tlv8:"2"`
}

// status is the write-response / read-response body.
type statusResponse struct {
	Status valueConfigStatus `tlv8:"1"`
}

type valueConfigStatus struct {
	IID            uint64               `tlv8:"1"`
	Parameters     transitionParameters `tlv8:"2"`
	TimeSinceStart uint64               `tlv8:"3"`
}

// buildStatusResponse encodes the write-response / read-response body that the
// controller returns after an UPDATE or READ command.
func buildStatusResponse(iid uint64, params transitionParameters, timeSinceStart uint64) ([]byte, error) {
	return tlv8.Marshal(statusResponse{
		Status: valueConfigStatus{
			IID:            iid,
			Parameters:     params,
			TimeSinceStart: timeSinceStart,
		},
	})
}

// startTimeMillis converts the 8-byte LE start-time buffer to epoch millis.
func startTimeMillis(buf []byte) int64 {
	var padded [8]byte
	copy(padded[:], buf)
	since2001 := binary.LittleEndian.Uint64(padded[:])
	return hapEpoch.UnixMilli() + int64(since2001)
}

package adaptive

import "math"

// curvePoint is one decoded transition entry. TransitionTime is the time in
// milliseconds to transition from the previous point to this one; Duration is
// how long this point holds before transitioning to the next.
type curvePoint struct {
	Temperature      float64
	AdjustmentFactor float64
	TransitionTime   int64
	Duration         int64
}

type brightnessRange struct {
	Min int
	Max int
}

// evaluate returns the colour temperature in mired for the given offset (ms
// since the curve start) and current brightness. ok is false when offset is
// past the end of the curve.
//
// Port of HAP-NodeJS getCurrentAdaptiveLightingTransitionPoint + scheduleNextUpdate.
func evaluate(curve []curvePoint, offsetMillis int64, brightness int, br brightnessRange) (int, bool) {
	if len(curve) < 2 {
		return 0, false
	}

	var lowerBoundTimeOffset int64
	var lower, upper *curvePoint
	var transitionOffset int64

	for i := 0; i+1 < len(curve); i++ {
		lb := curve[i]
		ub := curve[i+1]
		lowerBoundTimeOffset += lb.TransitionTime
		if offsetMillis >= lowerBoundTimeOffset &&
			offsetMillis <= lowerBoundTimeOffset+lb.Duration+ub.TransitionTime {
			lower = &curve[i]
			upper = &curve[i+1]
			transitionOffset = offsetMillis - lowerBoundTimeOffset
			break
		}
		lowerBoundTimeOffset += lb.Duration
	}

	if lower == nil || upper == nil {
		return 0, false
	}

	var temp, adj float64
	if lower.Duration > 0 && transitionOffset <= lower.Duration {
		temp = lower.Temperature
		adj = lower.AdjustmentFactor
	} else {
		pct := float64(transitionOffset-lower.Duration) / float64(upper.TransitionTime)
		temp = lower.Temperature + (upper.Temperature-lower.Temperature)*pct
		adj = lower.AdjustmentFactor + (upper.AdjustmentFactor-lower.AdjustmentFactor)*pct
	}

	b := brightness
	if b < br.Min {
		b = br.Min
	} else if b > br.Max {
		b = br.Max
	}

	return int(math.Round(temp + adj*float64(b))), true
}

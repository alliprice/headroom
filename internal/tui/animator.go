package tui

import "github.com/alliprice/headroom/internal/parse"

// barAnimFunc is a callback that returns animated (usage, glide, glideOpacity)
// values for a given bar key during the loading->running transition sweep.
// If nil, bars are rendered at their real values with full glide opacity.
type barAnimFunc func(key string, usage, glide float64) (animUsage, animGlide, glideOpacity float64)

// barAnimTarget holds the sweep target for a single bar during the
// choreographed loading animation.
type barAnimTarget struct {
	key     string
	usage   float64 // target usagePct
	glide   float64 // target glidePct
	startMs int     // ms offset from barStartFrame (0, 500, 1000, ...)
}

// animState tracks the state of the choreographed loading animation.
type animState struct {
	dataReady     bool            // fetchResultMsg received
	barAnimating  bool            // bar sweep phase active
	barStartFrame int             // sleepFrame when bar animation began
	barTargets    []barAnimTarget // per-bar sweep targets
}

// easeOutCubic applies a cubic ease-out curve to t in [0,1].
func easeOutCubic(t float64) float64 {
	t -= 1
	return t*t*t + 1
}

// allBarsFinished returns true once all bar sweep + glide fade animations
// have completed (last bar needs startMs + 1000ms sweep).
func (a *animState) allBarsFinished(sleepFrame int) bool {
	if len(a.barTargets) == 0 {
		return true
	}
	elapsedMs := (sleepFrame - a.barStartFrame) * plasmaFrameMs // 100ms per frame
	last := a.barTargets[len(a.barTargets)-1]
	return elapsedMs >= last.startMs+1000
}

// buildTargets populates barTargets from current categories and extra usage.
// All bars start animating at 200ms (after glide markers fade in).
func (a *animState) buildTargets(cats []parse.Category, extra *parse.ExtraUsage) {
	var targets []barAnimTarget
	for _, cat := range cats {
		usage := cat.Utilization
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
		targets = append(targets, barAnimTarget{
			key:     cat.Key,
			usage:   usage,
			glide:   glide,
			startMs: 200,
		})
	}
	if extra != nil {
		targets = append(targets, barAnimTarget{
			key:     "extra_usage",
			usage:   extra.Utilization,
			glide:   parse.CalcMonthGlide(),
			startMs: 200,
		})
	}
	a.barTargets = targets
}

// buildAnimFunc constructs a barAnimFunc for the current animation state.
// Returns nil if no animation is active.
func (a *animState) buildAnimFunc(sleepFrame int) barAnimFunc {
	if !a.barAnimating {
		return nil
	}
	elapsedMs := (sleepFrame - a.barStartFrame) * plasmaFrameMs
	targetMap := make(map[string]barAnimTarget, len(a.barTargets))
	for _, bt := range a.barTargets {
		targetMap[bt.key] = bt
	}
	return func(key string, usage, glide float64) (float64, float64, float64) {
		bt, ok := targetMap[key]
		if !ok {
			return usage, glide, 1.0
		}
		// Glide markers all fade in together over the first 200ms.
		opacity := float64(elapsedMs) / 200.0
		if opacity > 1 {
			opacity = 1
		}
		// Bar sweep starts at bt.startMs (200ms+ to let glide appear first).
		barElapsed := elapsedMs - bt.startMs
		if barElapsed <= 0 {
			return 0, bt.glide, opacity
		}
		sweepT := float64(barElapsed) / 1000.0
		if sweepT > 1 {
			sweepT = 1
		}
		eased := easeOutCubic(sweepT)
		animUsage := bt.usage * eased
		return animUsage, bt.glide, opacity
	}
}

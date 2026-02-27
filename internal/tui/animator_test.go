package tui

import (
	"math"
	"testing"

	"github.com/alliprice/headroom/internal/parse"
)

func TestEaseOutCubic_Endpoints(t *testing.T) {
	if got := easeOutCubic(0); got != 0 {
		t.Errorf("easeOutCubic(0) = %v, want 0", got)
	}
	if got := easeOutCubic(1); got != 1 {
		t.Errorf("easeOutCubic(1) = %v, want 1", got)
	}
}

func TestEaseOutCubic_Midpoint(t *testing.T) {
	mid := easeOutCubic(0.5)
	if mid <= 0 || mid >= 1 {
		t.Errorf("easeOutCubic(0.5) = %v, want value in (0, 1)", mid)
	}
	// Ease-out should be > 0.5 at the midpoint (decelerating curve)
	if mid <= 0.5 {
		t.Errorf("easeOutCubic(0.5) = %v, want > 0.5 for ease-out curve", mid)
	}
}

func TestAllBarsFinished_Empty(t *testing.T) {
	a := animState{}
	if !a.allBarsFinished(0) {
		t.Error("allBarsFinished with no targets should return true")
	}
}

func TestAllBarsFinished_InProgress(t *testing.T) {
	a := animState{
		barStartFrame: 0,
		barTargets: []barAnimTarget{
			{key: "a", startMs: 200},
		},
	}
	// At frame 5 = 500ms elapsed, need 200+1000=1200ms total
	if a.allBarsFinished(5) {
		t.Error("allBarsFinished at 500ms should return false (need 1200ms)")
	}
}

func TestAllBarsFinished_Complete(t *testing.T) {
	a := animState{
		barStartFrame: 0,
		barTargets: []barAnimTarget{
			{key: "a", startMs: 200},
		},
	}
	// At frame 12 = 1200ms elapsed, need 200+1000=1200ms
	if !a.allBarsFinished(12) {
		t.Error("allBarsFinished at 1200ms should return true")
	}
}

func TestBuildAnimFunc_NoAnimation(t *testing.T) {
	a := animState{barAnimating: false}
	if a.buildAnimFunc(0) != nil {
		t.Error("buildAnimFunc should return nil when not animating")
	}
}

func TestBuildAnimFunc_GlideFadeIn(t *testing.T) {
	a := animState{
		barAnimating:  true,
		barStartFrame: 0,
		barTargets: []barAnimTarget{
			{key: "test", usage: 50, glide: 40, startMs: 200},
		},
	}
	// At frame 1 = 100ms, glide opacity should be 0.5 (100/200)
	fn := a.buildAnimFunc(1)
	if fn == nil {
		t.Fatal("buildAnimFunc should return non-nil when animating")
	}
	_, _, opacity := fn("test", 50, 40)
	if math.Abs(opacity-0.5) > 0.01 {
		t.Errorf("opacity at 100ms = %v, want 0.5", opacity)
	}
}

func TestBuildAnimFunc_BarSweep(t *testing.T) {
	a := animState{
		barAnimating:  true,
		barStartFrame: 0,
		barTargets: []barAnimTarget{
			{key: "test", usage: 50, glide: 40, startMs: 200},
		},
	}
	// At frame 12 = 1200ms, bar has been sweeping for 1000ms (started at 200ms)
	// sweepT = 1.0, eased = 1.0, usage should be 50
	fn := a.buildAnimFunc(12)
	usage, _, _ := fn("test", 50, 40)
	if math.Abs(usage-50) > 0.01 {
		t.Errorf("usage at 1200ms = %v, want 50 (fully swept)", usage)
	}
}

func TestBuildAnimFunc_UnknownKey(t *testing.T) {
	a := animState{
		barAnimating:  true,
		barStartFrame: 0,
		barTargets: []barAnimTarget{
			{key: "test", usage: 50, glide: 40, startMs: 200},
		},
	}
	fn := a.buildAnimFunc(5)
	// Unknown key should return original values
	u, g, o := fn("unknown", 75, 60)
	if u != 75 || g != 60 || o != 1.0 {
		t.Errorf("unknown key returned (%v, %v, %v), want (75, 60, 1.0)", u, g, o)
	}
}

func TestBuildTargets(t *testing.T) {
	a := animState{}
	cats := []parse.Category{
		{Key: "cat1", Utilization: 30, ResetsAt: "", WindowSeconds: 3600},
		{Key: "cat2", Utilization: 60, ResetsAt: "", WindowSeconds: 3600},
	}
	a.buildTargets(cats, nil)
	if len(a.barTargets) != 2 {
		t.Fatalf("buildTargets produced %d targets, want 2", len(a.barTargets))
	}
	if a.barTargets[0].key != "cat1" {
		t.Errorf("target[0].key = %q, want %q", a.barTargets[0].key, "cat1")
	}
	if a.barTargets[0].startMs != 200 {
		t.Errorf("target[0].startMs = %d, want 200", a.barTargets[0].startMs)
	}
	if a.barTargets[1].key != "cat2" {
		t.Errorf("target[1].key = %q, want %q", a.barTargets[1].key, "cat2")
	}
}

func TestBuildTargets_WithExtra(t *testing.T) {
	a := animState{}
	cats := []parse.Category{
		{Key: "cat1", Utilization: 30},
	}
	extra := &parse.ExtraUsage{Utilization: 45}
	a.buildTargets(cats, extra)
	if len(a.barTargets) != 2 {
		t.Fatalf("buildTargets with extra produced %d targets, want 2", len(a.barTargets))
	}
	if a.barTargets[1].key != "extra_usage" {
		t.Errorf("target[1].key = %q, want %q", a.barTargets[1].key, "extra_usage")
	}
}

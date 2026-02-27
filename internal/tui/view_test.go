package tui

import (
	"flag"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alliprice/headroom/internal/parse"
	_ "github.com/alliprice/headroom/internal/provider"
)

var updateGolden = flag.Bool("update", false, "update .golden files")

func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *updateGolden {
		os.MkdirAll("testdata", 0o755)
		os.WriteFile(path, []byte(got), 0o644)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden file %s not found - run with -update to generate", path)
	}
	if got != string(want) {
		gotLines := strings.Split(got, "\n")
		wantLines := strings.Split(string(want), "\n")
		for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
			var g, w string
			if i < len(gotLines) {
				g = gotLines[i]
			}
			if i < len(wantLines) {
				w = wantLines[i]
			}
			if g != w {
				t.Fatalf("golden mismatch in %s at line %d:\n  got:  %q\n  want: %q\nrun with -update to regenerate", path, i+1, g, w)
				return
			}
		}
		t.Fatalf("golden mismatch in %s (different number of lines)\nrun with -update to regenerate", path)
	}
}

var frozenTime = time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)

func TestRenderBar_Golden(t *testing.T) {
	cases := []struct {
		name    string
		width   int
		usage   float64
		glide   float64
		opacity float64
	}{
		{"bar_50pct_80w", 80, 50.0, 40.0, 1.0},
		{"bar_90pct_80w", 80, 90.0, 60.0, 1.0},
		{"bar_10pct_40w", 40, 10.0, 30.0, 1.0},
		{"bar_0pct_80w", 80, 0.0, 25.0, 1.0},
		{"bar_100pct_80w", 80, 100.0, 80.0, 1.0},
		{"bar_half_opacity", 80, 50.0, 40.0, 0.5},
		{"bar_zero_opacity", 80, 50.0, 40.0, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RenderBar(tc.width, tc.usage, tc.glide, tc.opacity)
			assertGolden(t, "bar_"+tc.name, got)
		})
	}
}

func TestRenderPlasma_Golden(t *testing.T) {
	got := RenderPlasma(80, 24, 10, "press any key to wake")
	assertGolden(t, "plasma_80x24_f10", got)
}

func TestRenderLoadingFrame_Golden(t *testing.T) {
	grid := generateBgGrid(80, 24)
	got := RenderLoadingFrame(grid, 80, 24, 15, 0.75)
	assertGolden(t, "loading_frame_80x24_f15", got)
}

func TestGenerateBgGrid_Deterministic(t *testing.T) {
	g1 := generateBgGrid(80, 24)
	g2 := generateBgGrid(80, 24)
	if len(g1) != len(g2) {
		t.Fatalf("grid lengths differ: %d vs %d", len(g1), len(g2))
	}
	for i := range g1 {
		if g1[i] != g2[i] {
			t.Fatalf("grid cell %d differs: %+v vs %+v", i, g1[i], g2[i])
		}
	}
}

func testCategories() []parse.Category {
	return []parse.Category{
		{
			Key:           "five_hour",
			Name:          "Session (5hr)",
			Utilization:   42.5,
			ResetsAt:      "2025-06-15T16:00:00Z",
			WindowSeconds: 18000,
		},
		{
			Key:           "seven_day",
			Name:          "Weekly (7d)",
			Utilization:   67.3,
			ResetsAt:      "2025-06-20T12:00:00Z",
			WindowSeconds: 604800,
		},
	}
}

func testCodexCategories() []parse.Category {
	return []parse.Category{
		{
			Key:           "codex_primary",
			Name:          "Session (2hr)",
			Utilization:   31.0,
			ResetsAt:      "2025-06-15T14:00:00Z",
			WindowSeconds: 7200,
		},
		{
			Key:           "codex_secondary",
			Name:          "Weekly (3d)",
			Utilization:   52.8,
			ResetsAt:      "2025-06-18T12:00:00Z",
			WindowSeconds: 259200,
		},
	}
}

func testExtraUsage() *parse.ExtraUsage {
	return &parse.ExtraUsage{
		MonthlyLimit: 10000,
		UsedCredits:  4500,
		Utilization:  45.0,
	}
}

func buildTestModel(w, h int, st state, cats []parse.Category, extra map[string]*parse.ExtraUsage) Model {
	sched := newRefreshScheduler(0)
	ft := frozenTime
	sched.recordFetch(ft)

	m := Model{
		width:         w,
		height:        h,
		state:         st,
		categories:    cats,
		providerExtra: extra,
		keys:          newKeyMap(),
		available:     make(map[string]bool),
		sched:         sched,
		layout: &layoutInfo{
			panels: make(map[string]image.Rectangle),
			bars:   make(map[string][]barGeom),
		},
	}

	m.bgGrid = generateBgGrid(w, h)
	m.bgWidth = w
	m.bgHeight = h

	catsByProvider := m.groupCatsByProvider()
	if len(catsByProvider) > 0 {
		m.layoutState = defaultLayoutState(catsByProvider)
	} else {
		m.layoutState = layoutState{
			catOrder: make(map[string][]string),
			hidden:   make(map[string]bool),
		}
	}

	return m
}

func viewString(m Model) string {
	v := m.View()
	return v.Content
}

func TestView_RunningDual(t *testing.T) {
	parse.NowFunc = func() time.Time { return frozenTime }
	t.Cleanup(func() { parse.NowFunc = time.Now })

	cats := append(testCategories(), testCodexCategories()...)
	extra := map[string]*parse.ExtraUsage{
		"claude": testExtraUsage(),
	}
	m := buildTestModel(120, 40, stateRunning, cats, extra)
	assertGolden(t, "running_dual_120x40", viewString(m))
}

func TestView_RunningSingle(t *testing.T) {
	parse.NowFunc = func() time.Time { return frozenTime }
	t.Cleanup(func() { parse.NowFunc = time.Now })

	extra := map[string]*parse.ExtraUsage{
		"claude": testExtraUsage(),
	}
	m := buildTestModel(120, 40, stateRunning, testCategories(), extra)
	assertGolden(t, "running_single_120x40", viewString(m))
}

func TestView_RunningSmall(t *testing.T) {
	parse.NowFunc = func() time.Time { return frozenTime }
	t.Cleanup(func() { parse.NowFunc = time.Now })

	cats := append(testCategories(), testCodexCategories()...)
	extra := map[string]*parse.ExtraUsage{
		"claude": testExtraUsage(),
	}
	m := buildTestModel(30, 10, stateRunning, cats, extra)
	assertGolden(t, "running_small_30x10", viewString(m))
}

func TestView_Loading(t *testing.T) {
	m := buildTestModel(80, 24, stateLoading, nil, nil)
	m.sleepFrame = 15
	assertGolden(t, "loading_80x24_f15", viewString(m))
}

func TestView_Sleeping(t *testing.T) {
	m := buildTestModel(80, 24, stateSleeping, nil, nil)
	m.sleepFrame = 10
	assertGolden(t, "sleeping_80x24_f10", viewString(m))
}

func TestView_Error(t *testing.T) {
	parse.NowFunc = func() time.Time { return frozenTime }
	t.Cleanup(func() { parse.NowFunc = time.Now })

	extra := map[string]*parse.ExtraUsage{
		"claude": testExtraUsage(),
	}
	m := buildTestModel(120, 40, stateRunning, testCategories(), extra)
	m.errorMsg = "Failed to fetch usage data: connection timeout"
	assertGolden(t, "error_120x40", viewString(m))
}

func TestView_Empty(t *testing.T) {
	parse.NowFunc = func() time.Time { return frozenTime }
	t.Cleanup(func() { parse.NowFunc = time.Now })

	m := buildTestModel(120, 40, stateRunning, nil, nil)
	assertGolden(t, "empty_120x40", viewString(m))
}

func TestCompaction_Full(t *testing.T) {
	// 3 cats with extra, plenty of height (3 bars + 3 titles + 2 spacing + 3 extra = 11)
	c := computeCompaction(3, true, 20)
	if !c.showTitles || !c.showSpacing || !c.showExtra {
		t.Errorf("expected all true, got %+v", c)
	}
}

func TestCompaction_NoSpacing(t *testing.T) {
	// 3 cats: bars(3) + titles(3) = 6 fits in 6, but spacing needs 2 more = 8
	c := computeCompaction(3, false, 6)
	if !c.showTitles {
		t.Errorf("expected showTitles=true, got %+v", c)
	}
	if c.showSpacing {
		t.Errorf("expected showSpacing=false, got %+v", c)
	}
}

func TestCompaction_BarsOnly(t *testing.T) {
	// 3 cats with only 3 rows - just enough for bars
	c := computeCompaction(3, false, 3)
	if c.showTitles || c.showSpacing || c.showExtra {
		t.Errorf("expected all false, got %+v", c)
	}
}

func TestCompaction_ExtraFits(t *testing.T) {
	// 2 cats: bars(2) + titles(2) + spacing(1) + extra(3: bar+title+spacing) = 8
	c := computeCompaction(2, true, 8)
	if !c.showExtra {
		t.Errorf("expected showExtra=true, got %+v", c)
	}
}

func TestCompaction_ExtraDoesNotFit(t *testing.T) {
	// 2 cats: bars(2) + titles(2) + spacing(1) = 5, extra needs 3 more = 8, only 7 available
	c := computeCompaction(2, true, 7)
	if c.showExtra {
		t.Errorf("expected showExtra=false, got %+v", c)
	}
	if !c.showTitles || !c.showSpacing {
		t.Errorf("expected titles and spacing true, got %+v", c)
	}
}

func TestCompaction_SingleCategory(t *testing.T) {
	// 1 cat: bars(1) + titles(1) = 2, spacing = n-1 = 0
	c := computeCompaction(1, false, 2)
	if !c.showTitles {
		t.Errorf("expected showTitles=true, got %+v", c)
	}
	// n-1 = 0 so spacing adds nothing; should still be true
	if !c.showSpacing {
		t.Errorf("expected showSpacing=true (n-1=0), got %+v", c)
	}
}

func TestCompaction_ZeroCategories(t *testing.T) {
	c := computeCompaction(0, false, 10)
	if !c.showTitles || !c.showSpacing {
		t.Errorf("n=0 should allow titles and spacing (0 cost), got %+v", c)
	}
}

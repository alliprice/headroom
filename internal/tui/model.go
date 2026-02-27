package tui

import (
	"image"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/fetch"
	"github.com/alliprice/headroom/internal/parse"
)

type state int

const (
	stateRunning state = iota
	stateLoading
	stateSleeping
)

type inputMode int

const (
	inputNone inputMode = iota
	inputInterval
)

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

// Model is the Bubble Tea model for the headroom TUI.
type Model struct {
	// Data
	categories []parse.Category
	extra      *parse.ExtraUsage
	errorMsg   string
	isAuthErr  bool

	// Timing
	lastFetchTime    *time.Time // nil = never fetched
	lastFetchAttempt time.Time
	lastFocusTime    time.Time

	// UI config
	refreshFocused int // seconds, default 300

	// Window state
	width    int
	height   int
	hasFocus bool

	// Mode
	state      state
	sleepFrame int
	anim       animState

	// Inline prompt for "t" key (change interval)
	inputMode inputMode
	inputBuf  string

	// Capability
	codexAvailable bool

	// Help
	keys keyMap

	// Debug
	debugSleep bool

	// Background
	bgGrid   []bgCell
	bgWidth  int
	bgHeight int

	// Layout geometry (written by View, read by drag handlers in Phase 2)
	layout *layoutInfo

	// Layout customization (panel/bar ordering, hidden bars)
	layoutState layoutState

	// Drag state machine
	drag dragState
}

// layoutInfo tracks screen-space geometry of UI elements for hit-testing.
type layoutInfo struct {
	claudePanel image.Rectangle
	codexPanel  image.Rectangle
	statusBar   image.Rectangle
	claudeBars  []barGeom
	codexBars   []barGeom
}

// NewModel creates a new headroom TUI model.
func NewModel(debugSleep bool) Model {
	s := stateLoading
	if debugSleep {
		s = stateSleeping
	}

	return Model{
		refreshFocused: parse.RefreshFocused,
		hasFocus:       true,
		lastFocusTime:  time.Now(),
		state:          s,
		debugSleep:     debugSleep,
		keys:           newKeyMap(),
		layout:         &layoutInfo{},
	}
}

// Init implements tea.Model. It kicks off the initial data fetch and the tick timer.
func (m Model) Init() tea.Cmd {
	if m.debugSleep {
		// Start in sleep mode - no initial fetch (state already set in NewModel)
		return tea.Batch(plasmaTickCmd(), tea.RequestWindowSize)
	}

	// Probe for Codex availability; start plasma tick for loading animation
	return tea.Batch(
		m.probeCodex(),
		tea.RequestWindowSize,
		plasmaTickCmd(),
	)
}

// probeCodex checks if Codex is available, then triggers the initial fetch.
func (m Model) probeCodex() tea.Cmd {
	return func() tea.Msg {
		data, _ := fetch.FetchCodex()
		return codexProbeMsg{available: data != nil}
	}
}

// codexProbeMsg is sent after probing for Codex availability.
type codexProbeMsg struct {
	available bool
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.bgGrid = generateBgGrid(m.width, m.height)
		m.bgWidth = m.width
		m.bgHeight = m.height
		m.drag = dragState{} // cancel any in-progress drag on resize
		return m, nil

	case tea.FocusMsg:
		m.hasFocus = true
		m.lastFocusTime = time.Now()
		return m, nil

	case tea.BlurMsg:
		m.hasFocus = false
		return m, nil

	case codexProbeMsg:
		m.codexAvailable = msg.available
		return m, tea.Batch(doFetch(m.codexAvailable), tickCmd())

	case fetchResultMsg:
		m.categories = msg.categories
		m.extra = msg.extra
		m.errorMsg = msg.errorMsg
		m.isAuthErr = msg.isAuthErr
		m.lastFetchAttempt = time.Now()
		if !msg.fetchTime.IsZero() {
			t := msg.fetchTime
			m.lastFetchTime = &t
		}
		if m.state == stateLoading {
			// Signal data is ready but don't transition yet — wait for frame ≥40
			// in the sleepTickMsg handler to enforce the minimum 4s loading time.
			m.anim.dataReady = true
			return m, nil
		}
		// Normal running state: sync layout.
		var claudeKeys, codexKeys []string
		for _, c := range m.categories {
			if len(c.Key) > 6 && c.Key[:6] == "codex_" {
				codexKeys = append(codexKeys, c.Key)
			} else {
				claudeKeys = append(claudeKeys, c.Key)
			}
		}
		if len(m.layoutState.panelOrder) == 0 {
			m.layoutState = defaultLayoutState(claudeKeys, codexKeys)
		} else {
			m.layoutState.syncCategories(claudeKeys, codexKeys)
		}
		return m, nil

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd()) // re-arm the tick

		// Check if we should enter sleep mode
		if !m.hasFocus && m.state == stateRunning {
			elapsed := time.Since(m.lastFocusTime).Seconds()
			if elapsed >= float64(parse.SleepAfterUnfocusedSeconds) {
				m.state = stateSleeping
				cmds = append(cmds, plasmaTickCmd())
				return m, tea.Batch(cmds...)
			}
		}

		// Check if refresh is needed
		now := time.Now()
		if m.lastFetchTime != nil {
			interval := m.refreshFocused
			if !m.hasFocus {
				interval = parse.RefreshUnfocused
			}
			if now.Sub(*m.lastFetchTime).Seconds() >= float64(interval) {
				cmds = append(cmds, doFetch(m.codexAvailable))
			}
		} else if m.errorMsg != "" {
			var retryInterval int
			if m.isAuthErr {
				retryInterval = parse.RefreshOnAuthError
			} else {
				retryInterval = parse.RefreshFocused
			}
			if now.Sub(m.lastFetchAttempt).Seconds() >= float64(retryInterval) {
				cmds = append(cmds, doFetch(m.codexAvailable))
			}
		}

		return m, tea.Batch(cmds...)

	case sleepTickMsg:
		if m.state == stateLoading {
			m.sleepFrame++
			// Transition to running once data is ready AND minimum 4s elapsed (frame ≥40).
			if m.anim.dataReady && m.sleepFrame >= 40 {
				m.state = stateRunning
				m.anim.barAnimating = true
				m.anim.barStartFrame = m.sleepFrame
				// Populate bar targets — all start at 200ms (after glide markers fade in).
				var targets []barAnimTarget
				for _, cat := range m.categories {
					usage := cat.Utilization
					glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds)
					targets = append(targets, barAnimTarget{
						key:     cat.Key,
						usage:   usage,
						glide:   glide,
						startMs: 200,
					})
				}
				// Add extra usage bar if present.
				if m.extra != nil {
					targets = append(targets, barAnimTarget{
						key:     "extra_usage",
						usage:   m.extra.Utilization,
						glide:   parse.CalcMonthGlide(),
						startMs: 200,
					})
				}
				m.anim.barTargets = targets
				// Sync layout state.
				var claudeKeys, codexKeys []string
				for _, c := range m.categories {
					if len(c.Key) > 6 && c.Key[:6] == "codex_" {
						codexKeys = append(codexKeys, c.Key)
					} else {
						claudeKeys = append(claudeKeys, c.Key)
					}
				}
				if len(m.layoutState.panelOrder) == 0 {
					m.layoutState = defaultLayoutState(claudeKeys, codexKeys)
				} else {
					m.layoutState.syncCategories(claudeKeys, codexKeys)
				}
				return m, plasmaTickCmd() // keep ticking for bar animation
			}
			return m, plasmaTickCmd()
		}
		if m.state == stateRunning && m.anim.barAnimating {
			m.sleepFrame++
			// Stop ticking once all bars have finished animating.
			if m.allBarsFinished() {
				m.anim.barAnimating = false
				return m, nil
			}
			return m, plasmaTickCmd()
		}
		if m.state != stateSleeping {
			return m, nil
		}
		m.sleepFrame++
		return m, plasmaTickCmd()

	case tea.MouseClickMsg:
		if m.state == stateRunning && m.inputMode == inputNone && m.width >= 40 && m.height >= 12 {
			return m.handleMouseDown(msg)
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.drag.phase != dragIdle {
			return m.handleMouseMove(msg)
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if m.drag.phase != dragIdle {
			return m.handleMouseUp(msg)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// In input mode (interval prompt)
	if m.inputMode == inputInterval {
		return m.handleIntervalInput(msg)
	}

	// Loading mode: only q to quit
	if m.state == stateLoading {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

	// Sleep mode: any key wakes except q
	if m.state == stateSleeping {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		m.state = stateRunning
		m.hasFocus = true
		m.lastFocusTime = time.Now()
		// If this is the first wake (debug sleep), run the full init sequence:
		// probe codex availability and start the periodic tick timer.
		if m.lastFetchTime == nil && m.errorMsg == "" {
			return m, tea.Batch(m.probeCodex(), tickCmd())
		}
		return m, doFetch(m.codexAvailable)
	}

	// Normal mode
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Refresh):
		return m, doFetch(m.codexAvailable)
	case key.Matches(msg, m.keys.Interval):
		m.inputMode = inputInterval
		m.inputBuf = ""
		return m, nil
	case key.Matches(msg, m.keys.Reset):
		var claudeKeys, codexKeys []string
		for _, c := range m.categories {
			if len(c.Key) > 6 && c.Key[:6] == "codex_" {
				codexKeys = append(codexKeys, c.Key)
			} else {
				claudeKeys = append(claudeKeys, c.Key)
			}
		}
		m.layoutState = defaultLayoutState(claudeKeys, codexKeys)
		return m, nil
	}

	return m, nil
}

// allBarsFinished returns true once all bar sweep + glide fade animations
// have completed (last bar needs startMs + 1000ms sweep + 200ms glide fade).
func (m Model) allBarsFinished() bool {
	if len(m.anim.barTargets) == 0 {
		return true
	}
	elapsedMs := (m.sleepFrame - m.anim.barStartFrame) * 100 // 100ms per frame
	last := m.anim.barTargets[len(m.anim.barTargets)-1]
	// Last bar needs: startMs + 1000ms sweep (glide is already visible).
	return elapsedMs >= last.startMs+1000
}

// handleIntervalInput processes keyboard input while in interval-prompt mode.
func (m Model) handleIntervalInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		m.inputMode = inputNone
		m.inputBuf = ""
		return m, nil
	case "enter":
		// Parse the buffer as an integer
		val := 0
		for _, c := range m.inputBuf {
			if c >= '0' && c <= '9' {
				val = val*10 + int(c-'0')
			}
		}
		if val > 0 {
			m.refreshFocused = val
		}
		m.inputMode = inputNone
		m.inputBuf = ""
		return m, nil
	case "backspace":
		if len(m.inputBuf) > 0 {
			m.inputBuf = m.inputBuf[:len(m.inputBuf)-1]
		}
		return m, nil
	default:
		// Only accept digits
		if len(msg.String()) == 1 && msg.String()[0] >= '0' && msg.String()[0] <= '9' {
			m.inputBuf += msg.String()
		}
		return m, nil
	}
}


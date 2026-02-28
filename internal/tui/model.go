package tui

import (
	"image"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
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

// Model is the Bubble Tea model for the headroom TUI.
type Model struct {
	// Data
	categories    []parse.Category
	extra         *parse.ExtraUsage
	providerExtra map[string]*parse.ExtraUsage // provider ID → extra usage
	errorMsg      string

	// Refresh/sleep scheduling
	sched refreshScheduler

	// Window state
	width  int
	height int

	// Mode
	state      state
	sleepFrame int
	anim       animState

	// Inline prompt for "t" key (change interval)
	inputMode inputMode
	inputBuf  string

	// Provider availability
	available map[string]bool

	// Help
	keys keyMap

	// Debug
	debugSleep bool

	// Demo mode (--demo flag)
	demoMode  bool
	demoStep  demoStep
	demoFrame int
	demoEndX  int // target X for current drag animation (captured on frame 1)
	demoEndY  int // target Y for current drag animation (captured on frame 1)

	// Background
	bgGrid   []bgCell
	bgWidth  int
	bgHeight int

	// Layout geometry (written by View, read by drag handlers)
	layout *layoutInfo

	// Layout customization (panel/bar ordering, hidden bars)
	layoutState layoutState

	// Drag state machine
	drag dragState

	// Undo history for layout commands
	cmdHistory []layoutCmd
}

// layoutInfo tracks screen-space geometry of UI elements for hit-testing.
type layoutInfo struct {
	panels    map[string]image.Rectangle // provider ID → panel bounds
	bars      map[string][]barGeom       // provider ID → bar geometry
	statusBar image.Rectangle
	trashZone image.Rectangle
}

// NewModel creates a new headroom TUI model.
func NewModel(debugSleep, demo bool) Model {
	s := stateLoading
	if debugSleep {
		s = stateSleeping
	}

	return Model{
		sched:         newRefreshScheduler(0),
		state:         s,
		debugSleep:    debugSleep,
		demoMode:      demo,
		keys:          newKeyMap(),
		available:     make(map[string]bool),
		providerExtra: make(map[string]*parse.ExtraUsage),
		layout: &layoutInfo{
			panels: make(map[string]image.Rectangle),
			bars:   make(map[string][]barGeom),
		},
	}
}

// Init implements tea.Model. It kicks off the initial data fetch and the tick timer.
func (m Model) Init() tea.Cmd {
	if m.debugSleep {
		// Start in sleep mode - no initial fetch (state already set in NewModel)
		return tea.Batch(plasmaTickCmd(), tea.RequestWindowSize)
	}

	if m.demoMode {
		for _, p := range provider.All {
			m.available[p.ID] = true
		}
		return tea.Batch(mockFetch(), tea.RequestWindowSize, plasmaTickCmd())
	}

	// Probe for provider availability; start plasma tick for loading animation
	return tea.Batch(
		probeProviders(),
		tea.RequestWindowSize,
		plasmaTickCmd(),
	)
}

// probeProviders checks which providers with non-nil Probe are available.
func probeProviders() tea.Cmd {
	return func() tea.Msg {
		avail := make(map[string]bool)
		for _, p := range provider.All {
			if p.Probe == nil {
				avail[p.ID] = true
			} else {
				avail[p.ID] = p.Probe()
			}
		}
		return probeResultMsg{available: avail}
	}
}

// probeResultMsg is sent after probing all providers for availability.
type probeResultMsg struct {
	available map[string]bool
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
		m.sched.setFocus(true, time.Now())
		return m, nil

	case tea.BlurMsg:
		m.sched.setFocus(false, time.Now())
		return m, nil

	case probeResultMsg:
		m.available = msg.available
		return m, tea.Batch(doFetch(m.available), tickCmd())

	case fetchResultMsg:
		m.categories = msg.categories
		m.extra = msg.extra
		m.providerExtra = msg.providerExtra
		m.errorMsg = msg.errorMsg
		m.sched.recordFetchAttempt(time.Now())
		if msg.errorMsg != "" {
			m.sched.recordError(msg.isAuthErr)
		} else {
			m.sched.clearError()
		}
		if !msg.fetchTime.IsZero() {
			m.sched.recordFetch(msg.fetchTime)
		}
		if m.state == stateLoading {
			// Signal data is ready but don't transition yet - wait for frame ≥40
			// in the sleepTickMsg handler to enforce the minimum 4s loading time.
			m.anim.dataReady = true
			return m, nil
		}
		// Normal running state: sync layout.
		catsByProvider := m.groupCatsByProvider()
		if len(m.layoutState.panelOrder) == 0 {
			m.layoutState = defaultLayoutState(catsByProvider)
		} else {
			m.layoutState.syncCategories(catsByProvider)
		}
		return m, nil

	case tickMsg:
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd()) // re-arm the tick

		if m.state == stateRunning {
			switch m.sched.tick(time.Now()) {
			case refreshSleep:
				m.state = stateSleeping
				cmds = append(cmds, plasmaTickCmd())
			case refreshFetch:
				cmds = append(cmds, doFetch(m.available))
			}
		}

		return m, tea.Batch(cmds...)

	case demoTickMsg:
		return m.updateDemo()

	case sleepTickMsg:
		if m.state == stateLoading {
			m.sleepFrame++
			// Transition to running once data is ready AND minimum 4s elapsed (frame ≥40).
			if m.anim.dataReady && m.sleepFrame >= 40 {
				m.state = stateRunning
				m.anim.barAnimating = true
				m.anim.barStartFrame = m.sleepFrame
				m.anim.buildTargets(m.categories, m.extra)
				// Sync layout state.
				catsByProvider := m.groupCatsByProvider()
				if len(m.layoutState.panelOrder) == 0 {
					m.layoutState = defaultLayoutState(catsByProvider)
				} else {
					m.layoutState.syncCategories(catsByProvider)
				}
				return m, plasmaTickCmd() // keep ticking for bar animation
			}
			return m, plasmaTickCmd()
		}
		if m.state == stateRunning && m.anim.barAnimating {
			m.sleepFrame++
			// Stop ticking once all bars have finished animating.
			if m.anim.allBarsFinished(m.sleepFrame) {
				m.anim.barAnimating = false
				if m.demoMode {
					m.demoStep = demoWait1
					m.demoFrame = 0
					return m, demoTickCmd()
				}
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
		if m.demoMode && m.demoStep != demoIdle {
			return m, nil
		}
		if m.state == stateRunning && m.inputMode == inputNone && m.width >= 40 && m.height >= 12 {
			return m.handleMouseDown(msg)
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.demoMode && m.demoStep != demoIdle {
			return m, nil
		}
		if m.drag.phase != dragIdle {
			return m.handleMouseMove(msg)
		}
		return m, nil

	case tea.MouseReleaseMsg:
		if m.demoMode && m.demoStep != demoIdle {
			return m, nil
		}
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
	// Demo mode: only q quits
	if m.demoMode && m.demoStep != demoIdle {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}

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
		m.sched.setFocus(true, time.Now())
		// If this is the first wake (debug sleep), run the full init sequence:
		// probe provider availability and start the periodic tick timer.
		if m.sched.lastFetchTime == nil && m.errorMsg == "" {
			return m, tea.Batch(probeProviders(), tickCmd())
		}
		return m, doFetch(m.available)
	}

	// Normal mode
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Refresh):
		return m, doFetch(m.available)
	case key.Matches(msg, m.keys.Interval):
		m.inputMode = inputInterval
		m.inputBuf = ""
		return m, nil
	case key.Matches(msg, m.keys.Reset):
		prev := m.layoutState.clone()
		newLS := defaultLayoutState(m.groupCatsByProvider())
		cmd := restoreAllCmd{prevState: prev, newState: newLS}
		m.layoutState = cmd.newState.clone()
		m.pushCmd(cmd)
		return m, nil
	case key.Matches(msg, m.keys.Undo):
		m.undoCmd()
		return m, nil
	}

	return m, nil
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
			m.sched.setInterval(val)
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

// pushCmd appends a command to the undo history, capping at maxCmdHistory.
func (m *Model) pushCmd(cmd layoutCmd) {
	m.cmdHistory = append(m.cmdHistory, cmd)
	if len(m.cmdHistory) > maxCmdHistory {
		m.cmdHistory = m.cmdHistory[len(m.cmdHistory)-maxCmdHistory:]
	}
}

// undoCmd pops and reverses the last command in the undo history.
func (m *Model) undoCmd() {
	if len(m.cmdHistory) == 0 {
		return
	}
	last := m.cmdHistory[len(m.cmdHistory)-1]
	m.cmdHistory = m.cmdHistory[:len(m.cmdHistory)-1]
	last.Undo(&m.layoutState)
}

// groupCatsByProvider builds a provider ID → category keys map from the
// current categories using the provider registry. Categories whose keys
// match a provider's CategoryIDs go to that provider; unmatched keys go
// to the first provider (Claude).
func (m Model) groupCatsByProvider() map[string][]string {
	result := make(map[string][]string)
	// Build a lookup: category key → provider ID.
	keyToProvider := make(map[string]string)
	for _, p := range provider.All {
		for _, k := range p.CategoryIDs {
			keyToProvider[k] = p.ID
		}
	}
	for _, c := range m.categories {
		pid, ok := keyToProvider[c.Key]
		if !ok {
			// Prefix-based fallback: "gemini_foo" routes to the "gemini" provider.
			for _, p := range provider.All {
				if strings.HasPrefix(c.Key, p.ID+"_") {
					pid = p.ID
					ok = true
					break
				}
			}
		}
		if !ok {
			// Default to first provider for unknown keys.
			pid = provider.All[0].ID
		}
		result[pid] = append(result[pid], c.Key)
	}
	return result
}


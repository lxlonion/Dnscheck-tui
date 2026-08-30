// Package tui implements the bubbletea terminal UI with two tab pages:
// DNS performance dashboard and domain resolution / IP geo view.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"dnscheck/internal/config"
	"dnscheck/internal/dnsclient"
	"dnscheck/internal/geo"
)

const (
	TabDNS = iota
	TabGeo

	probeAttempts = 3
	geoWorkers    = 4
	defaultWidth  = 80
	defaultHeight = 24

	// wheelStep is how many lines one mouse wheel notch scrolls.
	wheelStep = 3
	// viewChrome is the vertical space View() spends outside the body:
	// header line, one blank line below the header, one above the footer
	// and the footer line itself.
	viewChrome = 4
)

type probeDoneMsg struct {
	runID   int
	results []dnsclient.ProbeResult
}

type page2DoneMsg struct {
	runID   int
	results []dnsclient.DomainResult
	geo     map[string]geo.Info
}

type probeStartMsg struct{}

// Model is the root bubbletea model. Page 1 (probe) and Page 2 (resolve +
// geo) run independently: each has its own run ID, cancel func and results.
// Page 2 opens a domain picker (↑/↓ move, Space toggle, Enter confirm)
// before its first run instead of testing every configured domain at once.
type Model struct {
	cfg    *config.Config
	tab    int
	width  int
	height int

	run1ID       int
	page1Running bool
	page1Results []dnsclient.ProbeResult
	cancel1      context.CancelFunc
	page1EverRan bool

	run2ID       int
	page2Running bool
	page2Results []dnsclient.DomainResult
	page2Geo     map[string]geo.Info
	cancel2      context.CancelFunc
	page2EverRan bool
	run2Domains  []string

	// Domain picker state for Page 2. selChecked parallels cfg.Domains.
	selActive  bool
	selCursor  int
	selChecked []bool
	selWarn    string

	// scroll is the body scroll offset (in lines) of the active page;
	// it is clamped against the rendered content in View.
	scroll int

	geoClient *geo.Client

	quitting bool
}

// New returns the initial model for the given configuration.
func New(cfg *config.Config) Model {
	return Model{
		cfg:       cfg,
		width:     defaultWidth,
		height:    defaultHeight,
		geoClient: geo.NewClient(),
	}
}

// Init kicks off the DNS performance probe automatically on launch by
// sending a start message through Update (so the model state transitions
// are applied on the live model, not a discarded copy).
func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return probeStartMsg{} }
}

func (m Model) probeDomain() string {
	return m.cfg.Domains[0]
}

type probeStart struct {
	model Model
	cmd   tea.Cmd
}

func (m Model) startProbe() probeStart {
	ctx, cancel := context.WithCancel(context.Background())
	m.run1ID++
	m.cancel1 = cancel
	m.page1Running = true
	m.page1EverRan = true
	m.page1Results = nil
	m.scroll = 0

	runID := m.run1ID
	servers := m.cfg.DNSServers
	question := m.probeDomain()
	timeout := m.cfg.Timeout()
	cmd := func() tea.Msg {
		return probeDoneMsg{
			runID:   runID,
			results: dnsclient.ProbeAll(ctx, servers, question, dnsclient.QTypeA, probeAttempts, timeout),
		}
	}
	return probeStart{model: m, cmd: cmd}
}

func (m Model) startResolve(domains []string) probeStart {
	if len(domains) == 0 {
		domains = m.cfg.Domains
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.run2ID++
	m.cancel2 = cancel
	m.page2Running = true
	m.page2EverRan = true
	m.page2Results = nil
	m.page2Geo = nil
	m.run2Domains = domains
	m.scroll = 0

	runID := m.run2ID
	servers := m.cfg.DNSServers
	timeout := m.cfg.Timeout()
	geoClient := m.geoClient
	cmd := func() tea.Msg {
		results := dnsclient.ResolveAll(ctx, servers, domains, timeout)
		geoInfos := geoClient.LookupMany(ctx, dnsclient.CollectIPs(results), geoWorkers)
		return page2DoneMsg{runID: runID, results: results, geo: geoInfos}
	}
	return probeStart{model: m, cmd: cmd}
}

// openSelector shows the Page 2 domain picker. Domains start fully checked
// so confirming immediately tests everything; re-opening keeps the previous
// choices for tweaking.
func (m Model) openSelector() Model {
	if len(m.selChecked) != len(m.cfg.Domains) {
		m.selChecked = make([]bool, len(m.cfg.Domains))
		for i := range m.selChecked {
			m.selChecked[i] = true
		}
	}
	m.selActive = true
	m.selWarn = ""
	m.scroll = 0
	return m
}

// scrollLines moves the active page's body offset by n lines. Only the
// lower bound is clamped here; the upper bound depends on the rendered
// content and is clamped in View.
func (m Model) scrollLines(n int) Model {
	m.scroll += n
	if m.scroll < 0 {
		m.scroll = 0
	}
	return m
}

// visibleHeight is how many body lines fit below the header and above the
// footer at the current terminal size.
func (m Model) visibleHeight() int {
	return max(1, m.height-viewChrome)
}

// maxScroll is an offset larger than any possible body height; End sets it
// and View clamps it to the actual bottom of the content.
const maxScroll = 1 << 30

// confirmSelection starts the Page 2 resolve run with the checked domains.
// Without any selection it keeps the picker open and sets a warning.
func (m Model) confirmSelection() probeStart {
	var domains []string
	for i, d := range m.cfg.Domains {
		if i < len(m.selChecked) && m.selChecked[i] {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		m.selWarn = "请至少选择一个域名再开始测试"
		return probeStart{model: m}
	}
	m.selActive = false
	m.selWarn = ""
	return m.startResolve(domains)
}

// Update handles key bindings, window resizing and test completion.
// [R] and [S] act on the active page's test only; the two pages never
// block or depend on each other.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case probeStartMsg:
		s := m.startProbe()
		return s.model, s.cmd

	case tea.MouseMsg:
		if m.selActive && m.tab == TabGeo {
			// The picker never scrolls: keep wheel input from leaking a
			// scroll offset back into the results view (D → wheel → Esc).
			return m, nil
		}
		// The input parser fills the legacy Type field for wheel events,
		// so matching on it alone covers SGR and X10 mouse input.
		switch msg.Type {
		case tea.MouseWheelUp:
			return m.scrollLines(-wheelStep), nil
		case tea.MouseWheelDown:
			return m.scrollLines(wheelStep), nil
		}
		return m, nil

	case probeDoneMsg:
		if msg.runID == m.run1ID {
			m.page1Results = msg.results
			m.page1Running = false
		}
		return m, nil

	case page2DoneMsg:
		if msg.runID == m.run2ID {
			m.page2Results = msg.results
			m.page2Geo = msg.geo
			m.page2Running = false
		}
		return m, nil

	case tea.KeyMsg:
		if m.selActive && m.tab == TabGeo {
			switch msg.String() {
			case "up", "down", " ", "spacebar", "enter", "esc":
				return m.updateSelector(msg)
			case "r", "R", "s", "S", "d", "D", "pgup", "pgdown", "home", "end":
				// Swallow test triggers and scrolling while picking
				// domains: the picker never scrolls, and a leaked offset
				// would move the results view after Esc.
				return m, nil
			}
			// q/ctrl+c, tab/1/2 fall through to the shared bindings below.
		}
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			if m.cancel1 != nil {
				m.cancel1()
			}
			if m.cancel2 != nil {
				m.cancel2()
			}
			return m, tea.Quit
		case "tab", "shift+tab":
			return m.switchTab((m.tab + 1) % 2)
		case "1":
			return m.switchTab(TabDNS)
		case "2":
			return m.switchTab(TabGeo)
		case "up":
			return m.scrollLines(-1), nil
		case "down":
			return m.scrollLines(1), nil
		case "pgup":
			return m.scrollLines(-m.visibleHeight()), nil
		case "pgdown":
			return m.scrollLines(m.visibleHeight()), nil
		case "home":
			m.scroll = 0
			return m, nil
		case "end":
			m.scroll = maxScroll // View clamps to the actual bottom
			return m, nil
		case "d", "D":
			if m.tab == TabGeo && !m.page2Running {
				return m.openSelector(), nil
			}
		case "r", "R":
			return m.restartActive()
		case "s", "S":
			return m.toggleActive()
		}
	}
	return m, nil
}

// updateSelector handles the picker's own keys (movement, toggle, confirm,
// dismiss). It is only reached when the picker is open on Page 2.
func (m Model) updateSelector(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.cfg.Domains)
	switch msg.String() {
	case "up":
		if n > 0 {
			m.selCursor = (m.selCursor + n - 1) % n
		}
	case "down":
		if n > 0 {
			m.selCursor = (m.selCursor + 1) % n
		}
	case " ", "spacebar":
		m.selWarn = ""
		if m.selCursor < len(m.selChecked) {
			m.selChecked[m.selCursor] = !m.selChecked[m.selCursor]
		}
	case "enter":
		c := m.confirmSelection()
		return c.model, c.cmd
	case "esc":
		m.selActive = false
	}
	return m, nil
}

func (m Model) switchTab(t int) (tea.Model, tea.Cmd) {
	m.tab = t
	m.scroll = 0
	if m.tab == TabGeo && !m.page2EverRan {
		// First entry opens the domain picker instead of auto-starting
		// the resolve run.
		return m.openSelector(), nil
	}
	return m, nil
}

func (m Model) restartActive() (tea.Model, tea.Cmd) {
	if m.tab == TabDNS {
		if m.cancel1 != nil {
			m.cancel1()
		}
		s := m.startProbe()
		return s.model, s.cmd
	}
	if !m.page2EverRan {
		return m.openSelector(), nil
	}
	if m.cancel2 != nil {
		m.cancel2()
	}
	s := m.startResolve(m.run2Domains)
	return s.model, s.cmd
}

func (m Model) toggleActive() (tea.Model, tea.Cmd) {
	if m.tab == TabDNS {
		if m.page1Running {
			m.cancel1()
			m.page1Running = false
			m.run1ID++
			return m, nil
		}
		s := m.startProbe()
		return s.model, s.cmd
	}
	if m.page2Running {
		m.cancel2()
		m.page2Running = false
		m.run2ID++
		return m, nil
	}
	if !m.page2EverRan {
		return m.openSelector(), nil
	}
	s := m.startResolve(m.run2Domains)
	return s.model, s.cmd
}

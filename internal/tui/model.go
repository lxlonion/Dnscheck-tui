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

func (m Model) startResolve() probeStart {
	ctx, cancel := context.WithCancel(context.Background())
	m.run2ID++
	m.cancel2 = cancel
	m.page2Running = true
	m.page2EverRan = true
	m.page2Results = nil
	m.page2Geo = nil

	runID := m.run2ID
	servers := m.cfg.DNSServers
	domains := m.cfg.Domains
	timeout := m.cfg.Timeout()
	geoClient := m.geoClient
	cmd := func() tea.Msg {
		results := dnsclient.ResolveAll(ctx, servers, domains, timeout)
		geoInfos := geoClient.LookupMany(ctx, dnsclient.CollectIPs(results), geoWorkers)
		return page2DoneMsg{runID: runID, results: results, geo: geoInfos}
	}
	return probeStart{model: m, cmd: cmd}
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
			m.tab = (m.tab + 1) % 2
			if m.tab == TabGeo && !m.page2EverRan {
				s := m.startResolve()
				return s.model, s.cmd
			}
			return m, nil
		case "r", "R":
			return m.restartActive()
		case "s", "S":
			return m.toggleActive()
		}
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
	if m.cancel2 != nil {
		m.cancel2()
	}
	s := m.startResolve()
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
	s := m.startResolve()
	return s.model, s.cmd
}

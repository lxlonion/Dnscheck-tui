// Package tui implements the bubbletea terminal UI with two tab pages:
// DNS performance dashboard and domain resolution / IP geo view.
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"dnscheck/internal/config"
	"dnscheck/internal/dnsclient"
)

const (
	TabDNS = iota
	TabGeo

	probeAttempts = 3
	defaultWidth  = 80
	defaultHeight = 24
)

type probeDoneMsg struct {
	runID   int
	results []dnsclient.ProbeResult
}

type probeStartMsg struct{}

// Model is the root bubbletea model.
type Model struct {
	cfg    *config.Config
	tab    int
	width  int
	height int

	runID        int
	page1Running bool
	page1Results []dnsclient.ProbeResult
	cancel       context.CancelFunc

	quitting bool
}

// New returns the initial model for the given configuration.
func New(cfg *config.Config) Model {
	return Model{cfg: cfg, width: defaultWidth, height: defaultHeight}
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
	m.runID++
	m.cancel = cancel
	m.page1Running = true
	m.page1Results = nil

	runID := m.runID
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

// Update handles key bindings, window resizing and probe completion.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case probeDoneMsg:
		if msg.runID == m.runID {
			m.page1Results = msg.results
			m.page1Running = false
		}
		return m, nil

	case probeStartMsg:
		s := m.startProbe()
		return s.model, s.cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "ctrl+c":
			m.quitting = true
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		case "tab", "shift+tab":
			m.tab = (m.tab + 1) % 2
			return m, nil
		case "r", "R":
			if m.cancel != nil {
				m.cancel()
			}
			s := m.startProbe()
			return s.model, s.cmd
		case "s", "S":
			if m.page1Running {
				m.cancel()
				m.page1Running = false
				return m, nil
			}
			s := m.startProbe()
			return s.model, s.cmd
		}
	}
	return m, nil
}

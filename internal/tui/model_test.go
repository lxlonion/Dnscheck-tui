package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dnscheck/internal/config"
	"dnscheck/internal/dnsclient"
	"dnscheck/internal/geo"
)

func testConfig() *config.Config {
	return &config.Config{
		TimeoutMs: 1500,
		DNSServers: []config.DNSServer{
			{Name: "Mock One", Address: "127.0.0.1:5300", Protocol: config.ProtocolUDP},
			{Name: "Mock Two", Address: "127.0.0.1:5301", Protocol: config.ProtocolTCP},
		},
		Domains: []string{"probe.example", "other.example"},
	}
}

func fakeResults() []dnsclient.ProbeResult {
	mk := func(name string, proto config.Protocol, rtt time.Duration, statuses ...dnsclient.Status) dnsclient.ProbeResult {
		s := config.DNSServer{Name: name, Address: "127.0.0.1:53", Protocol: proto}
		pr := dnsclient.ProbeResult{Server: s}
		for _, st := range statuses {
			r := dnsclient.Result{ServerName: name, Protocol: proto, Status: st, RTT: rtt}
			pr.Attempts = append(pr.Attempts, r)
			if r.Success() {
				pr.Successes++
				pr.AvgRTT += rtt
			} else {
				pr.Failures++
			}
		}
		pr.AvgRTT /= time.Duration(max(pr.Successes, 1))
		return pr
	}
	return []dnsclient.ProbeResult{
		mk("Mock One", config.ProtocolUDP, 12*time.Millisecond,
			dnsclient.StatusNOERROR, dnsclient.StatusNOERROR, dnsclient.StatusNOERROR),
		mk("Mock Two", config.ProtocolTCP, 40*time.Millisecond,
			dnsclient.StatusNOERROR, dnsclient.StatusTIMEOUT, dnsclient.StatusNOERROR),
	}
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestInitAutoStartEndToEnd(t *testing.T) {
	m := New(testConfig())

	startCmd := m.Init()
	msg := startCmd()
	if _, ok := msg.(probeStartMsg); !ok {
		t.Fatalf("Init returned %T, want probeStartMsg", msg)
	}
	m2, runCmd := m.Update(msg)
	mm := m2.(Model)
	if !mm.page1Running || mm.run1ID != 1 {
		t.Fatalf("after start msg: running=%v run1ID=%d, want true/1", mm.page1Running, mm.run1ID)
	}

	done := runCmd()
	d, ok := done.(probeDoneMsg)
	if !ok {
		t.Fatalf("probe cmd returned %T, want probeDoneMsg", done)
	}
	if d.runID != 1 {
		t.Errorf("done runID = %d, want 1", d.runID)
	}
	m3, _ := mm.Update(d)
	mmm := m3.(Model)
	if mmm.page1Running {
		t.Error("probe completion should clear running")
	}
	if len(mmm.page1Results) != len(mmm.cfg.DNSServers) {
		t.Fatalf("results = %d, want %d (first auto-probe results must be kept)", len(mmm.page1Results), len(mmm.cfg.DNSServers))
	}
}

func TestCapitalKeys(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, cmd := s.model.Update(keyMsg("R"))
	if !m2.(Model).page1Running || cmd == nil {
		t.Error("capital R should restart the probe")
	}
	m3, qcmd := m2.Update(keyMsg("Q"))
	if !m3.(Model).quitting || qcmd == nil {
		t.Error("capital Q should quit")
	}

	m4, stopCmd := m2.Update(keyMsg("S"))
	if m4.(Model).page1Running || stopCmd != nil {
		t.Error("capital S on running probe should stop it")
	}
	m5, startCmd := m4.Update(keyMsg("S"))
	if !m5.(Model).page1Running || startCmd == nil {
		t.Error("capital S on stopped probe should restart it")
	}
}

func TestCtrlCQuits(t *testing.T) {
	m := New(testConfig())
	m2, cmd := m.Update(keyMsg("ctrl+c"))
	if !m2.(Model).quitting || cmd == nil {
		t.Error("ctrl+c should quit")
	}
}

func TestShiftTabSwitch(t *testing.T) {
	m := New(testConfig())
	m2, _ := m.Update(keyMsg("shift+tab"))
	if m2.(Model).tab != TabGeo {
		t.Error("shift+tab should switch tabs")
	}
}

func TestFmtRTT(t *testing.T) {
	cases := map[time.Duration]string{
		900 * time.Microsecond:  "900µs",
		1500 * time.Microsecond: "1.5ms",
		12 * time.Millisecond:   "12.0ms",
		1200 * time.Millisecond: "1.20s",
	}
	for in, want := range cases {
		if got := fmtRTT(in); got != want {
			t.Errorf("fmtRTT(%v) = %q, want %q", in, got, want)
		}
	}
}

func stripAnsi(s string) string {
	return lipglossStripAnsi(s)
}

func TestInitialModel(t *testing.T) {
	m := New(testConfig())
	if m.tab != TabDNS {
		t.Errorf("initial tab = %d, want %d (Page 1 default)", m.tab, TabDNS)
	}
	if m.page1Running {
		t.Error("model should not be running before Init")
	}
}

func TestTabSwitch(t *testing.T) {
	m := New(testConfig())
	m2, _ := m.Update(keyMsg("tab"))
	if m2.(Model).tab != TabGeo {
		t.Errorf("after Tab: tab = %d, want %d", m2.(Model).tab, TabGeo)
	}
	m3, _ := m2.Update(keyMsg("tab"))
	if m3.(Model).tab != TabDNS {
		t.Errorf("second Tab should wrap back to %d, got %d", TabDNS, m3.(Model).tab)
	}
}

func TestQuitKey(t *testing.T) {
	m := New(testConfig())
	m2, cmd := m.Update(keyMsg("q"))
	if !m2.(Model).quitting {
		t.Error("q should mark quitting")
	}
	if cmd == nil {
		t.Error("q should return a quit command")
	}
}

func TestStartProbeState(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	if !s.model.page1Running {
		t.Error("startProbe should set running")
	}
	if s.model.run1ID != m.run1ID+1 {
		t.Errorf("runID = %d, want %d", s.model.run1ID, m.run1ID+1)
	}
	if s.cmd == nil {
		t.Fatal("startProbe should return a command")
	}
	msg := s.cmd()
	done, ok := msg.(probeDoneMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want probeDoneMsg", msg)
	}
	if len(done.results) != len(m.cfg.DNSServers) {
		t.Errorf("results = %d servers, want %d", len(done.results), len(m.cfg.DNSServers))
	}
	for _, r := range done.results {
		if r.Successes != 0 {
			t.Errorf("unreachable server should have 0 successes, got %d", r.Successes)
		}
	}

	m2, _ := s.model.Update(done)
	if m2.(Model).page1Running {
		t.Error("probeDoneMsg should clear running")
	}
	if m2.(Model).run1ID != done.runID {
		t.Error("runID mismatch")
	}
}

func TestStaleRunDiscarded(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, _ := s.model.Update(probeDoneMsg{runID: s.model.run1ID - 1, results: fakeResults()})
	if len(m2.(Model).page1Results) != 0 {
		t.Error("stale run results must be discarded")
	}
}

func TestViewPage1(t *testing.T) {
	m := New(testConfig())
	m.width = 120
	m.page1Results = fakeResults()
	m.page1Running = false
	out := stripAnsi(m.View())

	for _, want := range []string{
		"DNS 性能测试",
		"域名解析与 IP Geo",
		"DNS 节点性能仪表盘",
		"探测域名: probe.example",
		"Mock One",
		"Mock Two",
		"NOERROR",
		"100% (3/3)",
		"67% (2/3)",
		"TIMEOUT",
		"[Q] 退出程序",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q", want)
		}
	}
	if !strings.Contains(out, "12.0ms") {
		t.Errorf("View missing RTT 12.0ms: %s", out)
	}
}

func TestViewRunningPlaceholder(t *testing.T) {
	m := New(testConfig())
	m.page1Running = true
	out := stripAnsi(m.View())
	if !strings.Contains(out, "正在测试 DNS 服务器") {
		t.Errorf("running view: %s", out)
	}
}

func TestViewNoResultsPlaceholder(t *testing.T) {
	m := New(testConfig())
	out := stripAnsi(m.View())
	if !strings.Contains(out, "暂无结果，按 [R] 重新触发测试") {
		t.Errorf("empty view: %s", out)
	}
}

func fakePage2() ([]dnsclient.DomainResult, map[string]geo.Info) {
	mkSrv := func(name string, proto config.Protocol) config.DNSServer {
		return config.DNSServer{Name: name, Address: "127.0.0.1:53", Protocol: proto}
	}
	results := []dnsclient.DomainResult{
		{
			Domain: "probe.example",
			Entries: []dnsclient.ResolveResult{
				{Server: mkSrv("Mock One", config.ProtocolUDP), Domain: "probe.example",
					AStatus: dnsclient.StatusNOERROR, AAAAStatus: dnsclient.StatusNOERROR,
					A: []string{"93.184.216.34"}},
				{Server: mkSrv("Mock Two", config.ProtocolTCP), Domain: "probe.example",
					AStatus: dnsclient.StatusTIMEOUT, AAAAStatus: dnsclient.StatusTIMEOUT},
			},
		},
		{
			Domain: "other.example",
			Entries: []dnsclient.ResolveResult{
				{Server: mkSrv("Mock One", config.ProtocolUDP), Domain: "other.example",
					AStatus: dnsclient.StatusNOERROR, AAAAStatus: dnsclient.StatusNOERROR},
			},
		},
	}
	geoInfos := map[string]geo.Info{
		"93.184.216.34": {IP: "93.184.216.34", Country: "United States", CountryCode: "US", City: "Mountain View", ISP: "Google LLC"},
	}
	return results, geoInfos
}

func TestViewPage2(t *testing.T) {
	m := New(testConfig())
	m.tab = TabGeo
	m.width = 120
	m.page2Results, m.page2Geo = fakePage2()
	out := stripAnsi(m.View())

	for _, want := range []string{
		"域名解析与 IP Geo",
		"ip-api.com",
		"域名: probe.example",
		"域名: other.example",
		"93.184.216.34",
		"United States · Mountain View",
		"Google LLC",
		"N/A (TIMEOUT)",
		"N/A (No Answer)",
		"[Q] 退出程序",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("View missing %q", want)
		}
	}
}

func TestViewPage2Running(t *testing.T) {
	m := New(testConfig())
	m.tab = TabGeo
	m.page2Running = true
	out := stripAnsi(m.View())
	if !strings.Contains(out, "正在解析域名并查询 IP 地理位置") {
		t.Errorf("running view: %s", out)
	}
}

func TestPage2AutoStartOnTabEnter(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	m2, cmd := m.Update(keyMsg("tab"))
	mm := m2.(Model)
	if mm.tab != TabGeo || !mm.page2Running || !mm.page2EverRan {
		t.Fatalf("tab enter: tab=%d running=%v everRan=%v", mm.tab, mm.page2Running, mm.page2EverRan)
	}
	msg := cmd()
	d, ok := msg.(page2DoneMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want page2DoneMsg", msg)
	}
	if d.runID != 1 {
		t.Errorf("runID = %d, want 1", d.runID)
	}
	m3, _ := mm.Update(d)
	mmm := m3.(Model)
	if mmm.page2Running {
		t.Error("page2 completion should clear running")
	}
	if len(mmm.page2Results) != len(mmm.cfg.Domains) {
		t.Fatalf("results = %d domains, want %d", len(mmm.page2Results), len(mmm.cfg.Domains))
	}
}

func TestPage2SecondTabEnterDoesNotRestart(t *testing.T) {
	m := New(testConfig())
	m2, _ := m.Update(keyMsg("tab"))
	mm := m2.(Model)
	mm.page2Running = false
	mm.page2Results, _ = fakePage2()

	m3, cmd2 := mm.Update(keyMsg("tab"))
	mm3 := m3.(Model)
	if mm3.tab != TabDNS {
		t.Fatalf("tab = %d", mm3.tab)
	}
	m4, cmd3 := mm3.Update(keyMsg("tab"))
	mm4 := m4.(Model)
	if mm4.page2Running || cmd3 != nil {
		t.Error("re-entering Page 2 with existing results must not re-trigger")
	}
	_ = cmd2
}

func TestPageIndependentRunIDs(t *testing.T) {
	m := New(testConfig())
	s1 := m.startProbe()
	s2 := s1.model.startResolve()
	m2, _ := s2.model.Update(probeDoneMsg{runID: s1.model.run1ID, results: fakeResults()})
	m3, _ := m2.Update(page2DoneMsg{runID: s2.model.run2ID, results: nil, geo: nil})
	mm := m3.(Model)
	if len(mm.page1Results) == 0 {
		t.Error("page1 results must be accepted while page2 runs")
	}
	if mm.page2Running {
		t.Error("page2 done must clear page2 running")
	}
}

func TestRestartActivePerPage(t *testing.T) {
	m := New(testConfig())
	s := m.startResolve()
	mm := s.model
	mm.tab = TabGeo
	mm.page2Running = false

	m2, cmd := mm.Update(keyMsg("R"))
	if !m2.(Model).page2Running || cmd == nil {
		t.Error("R on Page 2 should restart resolve test")
	}
	if m2.(Model).run2ID != s.model.run2ID+1 {
		t.Errorf("run2ID = %d, want %d", m2.(Model).run2ID, s.model.run2ID+1)
	}

	m3, cmd2 := mm.Update(keyMsg("S"))
	if !m3.(Model).page2Running || cmd2 == nil {
		t.Error("S on stopped Page 2 should start it")
	}
}

func TestStopDiscardsLateResults(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, _ := s.model.Update(keyMsg("s"))
	mm := m2.(Model)
	if mm.page1Running {
		t.Fatal("probe should be stopped")
	}
	m3, _ := mm.Update(probeDoneMsg{runID: s.model.run1ID, results: fakeResults()})
	if len(m3.(Model).page1Results) != 0 {
		t.Error("late results from a stopped run must be discarded")
	}

	s2 := m.startResolve()
	s2.model.tab = TabGeo
	m4, _ := s2.model.Update(keyMsg("s"))
	mm4 := m4.(Model)
	m5, _ := mm4.Update(page2DoneMsg{runID: s2.model.run2ID, results: nil, geo: nil})
	if len(m5.(Model).page2Results) != 0 {
		t.Error("late page2 results from a stopped run must be discarded")
	}
}

func TestViewResizeAdaptive(t *testing.T) {
	m := New(testConfig())
	m.page1Results = fakeResults()
	for _, size := range []tea.WindowSizeMsg{
		{Width: 200, Height: 60},
		{Width: 120, Height: 40},
		{Width: 60, Height: 20},
		{Width: 20, Height: 5},
	} {
		m2, _ := m.Update(size)
		mm := m2.(Model)
		if mm.width != size.Width || mm.height != size.Height {
			t.Errorf("size %v not stored", size)
		}
		out := mm.View()
		if out == "" {
			t.Errorf("View empty at %v", size)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if visible := visibleWidth(line); visible > size.Width {
				t.Errorf("line width %d exceeds terminal %d at resize: %q", visible, size.Width, line)
			}
		}
	}
}

func TestViewResizeColumnPriority(t *testing.T) {
	m := New(testConfig())
	m.page1Results = fakeResults()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	out := stripAnsi(m2.(Model).View())
	for _, want := range []string{"状态", "成功率", "平均", "协议"} {
		if !strings.Contains(out, want) {
			t.Errorf("width 60 should keep %q column:\n%s", want, out)
		}
	}
	if strings.Contains(out, "RTT #3") {
		t.Error("width 60 should drop per-attempt RTT columns")
	}
}

func TestPauseKeyStopsRunning(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, cmd := s.model.Update(keyMsg("s"))
	mm := m2.(Model)
	if mm.page1Running {
		t.Error("S should stop a running probe")
	}
	if cmd != nil {
		t.Error("stop should not start a new command")
	}

	m3, cmd2 := mm.Update(keyMsg("s"))
	if !m3.(Model).page1Running || cmd2 == nil {
		t.Error("S on stopped probe should restart it")
	}
	m4, _ := m3.Update(keyMsg("r"))
	if !m4.(Model).page1Running {
		t.Error("R should restart probe")
	}
}

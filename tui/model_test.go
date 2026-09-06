package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"dnscheck/config"
	"dnscheck/dnsclient"
	"dnscheck/geo"
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
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
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

func TestHeaderUnderlineOnlyOnActiveTab(t *testing.T) {
	m := New(testConfig())
	m.width = 200
	dns := "  [Tab / 1] DNS 性能测试"
	geo := "  [Tab / 2] 域名解析与 IP Geo"

	want := lipgloss.JoinHorizontal(lipgloss.Top, tabActiveStyle.Render(dns), subtleStyle.Render(geo))
	if got := m.headerView(); got != want {
		t.Errorf("page 1 header = %q, want %q", got, want)
	}

	m.tab = TabGeo
	want = lipgloss.JoinHorizontal(lipgloss.Top, subtleStyle.Render(dns), tabActiveStyle.Render(geo))
	if got := m.headerView(); got != want {
		t.Errorf("page 2 header = %q, want %q", got, want)
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

// picker opens the Page 2 domain picker the way the Tab key would.
func picker(m Model) Model {
	m2, _ := m.Update(keyMsg("tab"))
	return m2.(Model)
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

func TestViewPage2IdleFooterAndStatus(t *testing.T) {
	m := New(testConfig())
	m.tab = TabGeo
	m.width = 120
	out := stripAnsi(m.View())
	if !strings.Contains(out, "[D] 选择域名") {
		t.Errorf("idle Page 2 footer should advertise [D]:\n%s", out)
	}
	if !strings.Contains(out, "未开始") {
		t.Errorf("Page 2 must show 未开始 before the first run:\n%s", out)
	}
	if strings.Contains(out, "已完成") {
		t.Error("Page 2 must not claim 已完成 before the first run")
	}

	m2, _ := m.Update(keyMsg("d"))
	out2 := stripAnsi(m2.(Model).View())
	if strings.Contains(out2, "[D] 选择域名") {
		t.Error("picker footer should show picker hints, not [D]")
	}
	for _, want := range []string{"[空格] 勾选/取消", "[回车] 开始测试", "[Esc] 取消"} {
		if !strings.Contains(out2, want) {
			t.Errorf("picker footer missing %q:\n%s", want, out2)
		}
	}
}

func TestPage2FirstEnterOpensSelector(t *testing.T) {
	m := New(testConfig())
	m.width = 120
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m2, cmd := m.Update(keyMsg("tab"))
	mm := m2.(Model)
	if mm.tab != TabGeo || !mm.selActive || mm.page2Running || mm.page2EverRan {
		t.Fatalf("tab enter: tab=%d selActive=%v running=%v everRan=%v",
			mm.tab, mm.selActive, mm.page2Running, mm.page2EverRan)
	}
	if cmd != nil {
		t.Error("first entry must open the picker, not start the test")
	}
	if len(mm.selChecked) != len(mm.cfg.Domains) {
		t.Fatalf("selChecked = %d, want %d (all pre-checked)", len(mm.selChecked), len(mm.cfg.Domains))
	}
	for i, c := range mm.selChecked {
		if !c {
			t.Errorf("selChecked[%d] = false, want pre-checked", i)
		}
	}

	out := stripAnsi(mm.View())
	for _, want := range []string{
		"选择要测试解析的域名",
		"已选 2 / 2",
		"> [x] probe.example",
		"[x] other.example",
		"[↑/↓] 移动",
		"[回车] 开始测试",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("selector view missing %q:\n%s", want, out)
		}
	}

	// Confirming the untouched selection starts the full run.
	m3, cmd2 := mm.Update(keyMsg("enter"))
	mm3 := m3.(Model)
	if !mm3.page2Running || !mm3.page2EverRan || mm3.selActive {
		t.Fatalf("confirm: running=%v everRan=%v selActive=%v", mm3.page2Running, mm3.page2EverRan, mm3.selActive)
	}
	if len(mm3.run2Domains) != len(mm3.cfg.Domains) {
		t.Errorf("run2Domains = %d, want %d", len(mm3.run2Domains), len(mm3.cfg.Domains))
	}
	d, ok := cmd2().(page2DoneMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want page2DoneMsg", cmd2())
	}
	if d.runID != 1 {
		t.Errorf("runID = %d, want 1", d.runID)
	}
	m4, _ := mm3.Update(d)
	mmm := m4.(Model)
	if mmm.page2Running {
		t.Error("page2 completion should clear running")
	}
	if len(mmm.page2Results) != len(mmm.cfg.Domains) {
		t.Fatalf("results = %d domains, want %d", len(mmm.page2Results), len(mmm.cfg.Domains))
	}
}

func TestSelectorNavigationAndToggle(t *testing.T) {
	m := New(testConfig())
	mm, _ := m.switchTab(TabGeo)
	mm = mm.(Model)

	mm2, _ := mm.Update(keyMsg("down"))
	if got := mm2.(Model).selCursor; got != 1 {
		t.Errorf("down: cursor = %d, want 1", got)
	}
	mm3, _ := mm2.Update(keyMsg("down"))
	if got := mm3.(Model).selCursor; got != 0 {
		t.Errorf("down wraps: cursor = %d, want 0", got)
	}
	mm4, _ := mm3.Update(keyMsg("up"))
	if got := mm4.(Model).selCursor; got != 1 {
		t.Errorf("up wraps: cursor = %d, want 1", got)
	}

	mm5, _ := mm4.Update(keyMsg(" "))
	five := mm5.(Model)
	if five.selChecked[1] {
		t.Error("space should uncheck the highlighted domain")
	}
	if !five.selChecked[0] {
		t.Error("other domains must keep their check state")
	}
	out := stripAnsi(five.View())
	if !strings.Contains(out, "> [ ] other.example") {
		t.Errorf("unchecked cursor line not rendered:\n%s", out)
	}
	if !strings.Contains(out, "已选 1 / 2") {
		t.Errorf("selected counter not updated:\n%s", out)
	}
}

func TestSelectorConfirmStartsSelectedOnly(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	mm := picker(m)

	// Highlight the second domain and uncheck it, then confirm.
	mm2, _ := mm.Update(keyMsg("down"))
	mm3, _ := mm2.Update(keyMsg(" "))
	m4, cmd := mm3.Update(keyMsg("enter"))
	confirmed := m4.(Model)
	if !confirmed.page2Running {
		t.Fatal("confirm should start the resolve run")
	}
	if len(confirmed.run2Domains) != 1 || confirmed.run2Domains[0] != "probe.example" {
		t.Fatalf("run2Domains = %v, want [probe.example]", confirmed.run2Domains)
	}
	d := cmd().(page2DoneMsg)
	m5, _ := confirmed.Update(d)
	if got := len(m5.(Model).page2Results); got != 1 {
		t.Fatalf("results = %d domains, want 1", got)
	}
	if m5.(Model).page2Results[0].Domain != "probe.example" {
		t.Errorf("resolved domain = %q, want probe.example", m5.(Model).page2Results[0].Domain)
	}
}

func TestSelectorEnterWithoutSelectionWarns(t *testing.T) {
	m := New(testConfig())
	mm := picker(m)
	mm2, _ := mm.Update(keyMsg(" "))
	mm3, _ := mm2.Update(keyMsg("down"))
	mm4, _ := mm3.Update(keyMsg(" "))
	warned, cmd := mm4.Update(keyMsg("enter"))
	wm := warned.(Model)
	if cmd != nil || wm.page2Running || wm.page2EverRan {
		t.Fatal("enter with nothing selected must not start the test")
	}
	if !wm.selActive || wm.selWarn == "" {
		t.Fatalf("warn = %q selActive = %v, want a warning and the picker to stay open", wm.selWarn, wm.selActive)
	}
	if !strings.Contains(stripAnsi(wm.View()), wm.selWarn) {
		t.Error("warning must be visible in the picker view")
	}
	// Toggling clears the warning; Enter now starts.
	mm5, _ := wm.Update(keyMsg(" "))
	if mm5.(Model).selWarn != "" {
		t.Error("toggling should clear the warning")
	}
	mm6, cmd2 := mm5.Update(keyMsg("enter"))
	f := mm6.(Model)
	if !f.page2Running || cmd2 == nil || len(f.run2Domains) != 1 {
		t.Errorf("re-toggled selection must start with 1 domain, got %v", f.run2Domains)
	}
}

func TestSelectorEscThenRReopens(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	mm := picker(m)

	mm2, _ := mm.Update(keyMsg("esc"))
	idle := mm2.(Model)
	if idle.selActive || idle.page2Running {
		t.Fatal("esc should dismiss the picker without starting")
	}
	m3, cmd := idle.Update(keyMsg("R"))
	reopened := m3.(Model)
	if !reopened.selActive || cmd != nil {
		t.Fatal("R before the first run should reopen the picker")
	}

	// After a completed run, Esc leaves results on screen and D reopens.
	m4, cmd2 := reopened.Update(keyMsg("enter"))
	running := m4.(Model)
	m4b, _ := running.Update(cmd2().(page2DoneMsg))
	finished := m4b.(Model)
	if finished.selActive {
		t.Error("picker must stay closed while results are shown")
	}
	m5, _ := finished.Update(keyMsg("d"))
	if !m5.(Model).selActive {
		t.Error("D should reopen the picker after a run")
	}
}

func TestSelectorSwallowsTestTriggers(t *testing.T) {
	m := New(testConfig())
	mm := picker(m)
	for _, key := range []string{"r", "R", "s", "S", "d", "D"} {
		m2, cmd := mm.Update(keyMsg(key))
		f := m2.(Model)
		if f.page2Running || f.page2EverRan || cmd != nil {
			t.Errorf("key %q while picking domains must not start the test", key)
		}
		if !f.selActive {
			t.Errorf("key %q must not close the picker", key)
		}
	}
	// Page switching still works and returns to the open picker.
	m3, _ := mm.Update(keyMsg("1"))
	if m3.(Model).tab != TabDNS {
		t.Error("key 1 should still switch to Page 1 while picking")
	}
	m4, _ := m3.Update(keyMsg("tab"))
	f := m4.(Model)
	if f.tab != TabGeo || !f.selActive {
		t.Error("returning to Page 2 must keep the picker open")
	}
}

func TestScrollKeysAndMouse(t *testing.T) {
	m := New(testConfig())
	// Give the body real overflow: duplicated results render 4 table rows
	// (9 body lines); height 8 leaves 4 visible lines below the chrome.
	m.page1Results = append(fakeResults(), fakeResults()...)
	m.height = 8
	maxOff := m.maxScrollOffset()
	if maxOff <= m.visibleHeight() {
		t.Fatalf("precondition: maxScrollOffset = %d, want more than the %d visible lines",
			maxOff, m.visibleHeight())
	}

	m2, _ := m.Update(keyMsg("down"))
	if got := m2.(Model).scroll; got != 1 {
		t.Errorf("down: scroll = %d, want 1", got)
	}
	m3, _ := m2.Update(keyMsg("up"))
	if got := m3.(Model).scroll; got != 0 {
		t.Errorf("up: scroll = %d, want 0", got)
	}
	m4, _ := m3.Update(keyMsg("up"))
	if got := m4.(Model).scroll; got != 0 {
		t.Errorf("up below top must clamp, scroll = %d", got)
	}

	m5, _ := m4.Update(tea.MouseMsg{Type: tea.MouseWheelDown})
	if got := m5.(Model).scroll; got != wheelStep {
		t.Errorf("wheel down: scroll = %d, want %d", got, wheelStep)
	}
	m6, _ := m5.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	if got := m6.(Model).scroll; got != 0 {
		t.Errorf("wheel up: scroll = %d, want 0", got)
	}

	m7, _ := m6.Update(keyMsg("pgup"))
	if got := m7.(Model).scroll; got != 0 {
		t.Errorf("pgup below top must clamp, scroll = %d", got)
	}
	m8, _ := m7.Update(keyMsg("pgdown"))
	if got := m8.(Model).scroll; got != m.visibleHeight() {
		t.Errorf("pgdown: scroll = %d, want %d", got, m.visibleHeight())
	}
	m9, _ := m8.Update(keyMsg("end"))
	if got := m9.(Model).scroll; got != maxOff {
		t.Errorf("end: scroll = %d, want the real bottom %d", got, maxOff)
	}
	m10, _ := m9.Update(keyMsg("down"))
	if got := m10.(Model).scroll; got != maxOff {
		t.Errorf("down below bottom must clamp, scroll = %d, want %d", got, maxOff)
	}
	m11, _ := m10.Update(keyMsg("home"))
	if got := m11.(Model).scroll; got != 0 {
		t.Errorf("home: scroll = %d, want 0", got)
	}
}

// Regression: End used to store a huge sentinel offset that only View
// clamped on its render copy, so every later scroll-up counted down from
// 2^30, stayed past the real bottom and the pane never moved.
func TestEndThenScrollUpStillMoves(t *testing.T) {
	m := New(testConfig())
	m.page1Results = fakeResults()
	m.height = 8 // 4 visible lines for a 7-line body, so maxOffset = 3

	m2, _ := m.Update(keyMsg("end"))
	bottom := m2.(Model)
	if bottom.scroll != bottom.maxScrollOffset() || bottom.scroll == 0 {
		t.Fatalf("end: scroll = %d, want the real bottom %d", bottom.scroll, bottom.maxScrollOffset())
	}
	if out := stripAnsi(bottom.View()); !strings.Contains(out, "Mock Two") {
		t.Errorf("end must reveal the bottom row:\n%s", out)
	}

	m3, _ := bottom.Update(keyMsg("up"))
	oneUp := m3.(Model)
	if got := oneUp.scroll; got != bottom.scroll-1 {
		t.Errorf("up after end: scroll = %d, want %d", got, bottom.scroll-1)
	}
	if out := stripAnsi(oneUp.View()); strings.Contains(out, "Mock Two") {
		t.Errorf("up after end must move the view off the bottom:\n%s", out)
	}

	m4, _ := oneUp.Update(tea.MouseMsg{Type: tea.MouseWheelUp})
	if got := m4.(Model).scroll; got != 0 {
		t.Errorf("wheel up after end: scroll = %d, want 0", got)
	}
	m5, _ := m4.Update(keyMsg("pgup"))
	if got := m5.(Model).scroll; got != 0 {
		t.Errorf("pgup after end: scroll = %d, want 0", got)
	}
}

func TestPickerSwallowsScrollInput(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	mm := picker(m)
	m2, cmd := mm.Update(keyMsg("enter"))
	m3, _ := m2.Update(cmd().(page2DoneMsg))
	finished := m3.(Model)

	// Reopen the picker over the results, scroll away with every input
	// path, then dismiss: the offset must not leak into the results view.
	m4, _ := finished.Update(keyMsg("d"))
	inPicker := m4.(Model)
	if !inPicker.selActive {
		t.Fatal("D should reopen the picker")
	}
	for _, msg := range []tea.Msg{
		tea.MouseMsg{Type: tea.MouseWheelDown},
		tea.MouseMsg{Type: tea.MouseWheelDown},
		keyMsg("pgdown"),
		keyMsg("end"),
		keyMsg("pgup"),
	} {
		m5, _ := inPicker.Update(msg)
		inPicker = m5.(Model)
		if got := inPicker.scroll; got != 0 {
			t.Fatalf("scroll input while picking must be ignored, scroll = %d", got)
		}
		if !inPicker.selActive {
			t.Fatal("scroll input must not close the picker")
		}
	}
	m6, _ := inPicker.Update(keyMsg("esc"))
	dismissed := m6.(Model)
	if dismissed.selActive {
		t.Error("esc should dismiss the picker")
	}
	if got := dismissed.scroll; got != 0 {
		t.Errorf("dismissed picker must leave the results at the top, scroll = %d", got)
	}
}

func TestScrollResetsOnTabSwitchAndNewRun(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	// Overflow Page 1 so "end" produces a non-zero offset to reset from.
	m.page1Results = fakeResults()
	m.height = 8

	scrolled, _ := m.Update(keyMsg("end"))
	if got := scrolled.(Model).scroll; got == 0 {
		t.Fatal("precondition: end should scroll Page 1 to the bottom")
	}
	m2, _ := scrolled.Update(keyMsg("tab"))
	if got := m2.(Model).scroll; got != 0 {
		t.Errorf("tab switch must reset scroll, got %d", got)
	}

	m3, _ := m2.Update(keyMsg("end")) // swallowed by the open picker
	m4, _ := m3.Update(keyMsg("tab")) // back to Page 1 with results
	m5, cmd := m4.Update(keyMsg("R"))
	if cmd == nil {
		t.Fatal("R should restart the Page 1 probe")
	}
	if got := m5.(Model).scroll; got != 0 {
		t.Errorf("new run must reset scroll, got %d", got)
	}

	// The new run cleared the results; restore them so "end" scrolls again.
	m5b := m5.(Model)
	m5b.page1Results = fakeResults()
	m6, _ := m5b.Update(keyMsg("end"))
	if got := m6.(Model).scroll; got == 0 {
		t.Fatal("precondition: end should scroll again after the restart")
	}
	m7, _ := m6.Update(keyMsg("2"))
	mm7 := m7.(Model)
	if !mm7.selActive {
		t.Fatal("first Page 2 entry must show the picker")
	}
	m8, cmd2 := mm7.Update(keyMsg("enter"))
	if cmd2 == nil {
		t.Fatal("confirm should start the run")
	}
	if got := m8.(Model).scroll; got != 0 {
		t.Errorf("confirming a run must reset scroll, got %d", got)
	}
}

func TestViewPage2ScrollsByHeight(t *testing.T) {
	m := New(testConfig())
	m.tab = TabGeo
	m.width = 120
	m.height = 12 // 8 body lines visible
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	m.page2Results, m.page2Geo = fakePage2()

	first := stripAnsi(m.View())
	if !strings.Contains(first, "域名: probe.example") {
		t.Errorf("top of Page 2 body must be visible:\n%s", first)
	}
	if strings.Contains(first, "域名: other.example") {
		t.Error("second domain must be below the fold at height 12")
	}
	if !strings.Contains(first, "滚动 0/") {
		t.Errorf("footer must show the scroll position when content overflows:\n%s", first)
	}
	if !strings.Contains(first, "[↑/↓] 滚动") {
		t.Errorf("footer must advertise scrolling when content overflows:\n%s", first)
	}

	m2, _ := m.Update(keyMsg("end"))
	last := stripAnsi(m2.(Model).View())
	if !strings.Contains(last, "域名: other.example") {
		t.Errorf("end must reveal the bottom of the body:\n%s", last)
	}
}

func TestViewPickerNeverScrolls(t *testing.T) {
	m := New(testConfig())
	m.width = 120
	m.height = 8 // would clip the picker if it were scrollable
	mm := picker(m)

	out := stripAnsi(mm.View())
	if !strings.Contains(out, "other.example") {
		t.Error("domain picker must always render in full, never scroll")
	}
}

func TestRerunReusesSelectedDomains(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	mm := picker(m)

	mm2, _ := mm.Update(keyMsg("down"))
	mm3, _ := mm2.Update(keyMsg(" "))
	m4, cmd := mm3.Update(keyMsg("enter"))
	f := m4.(Model)
	f.page2Running = false
	f.Update(cmd().(page2DoneMsg))

	m5, cmd2 := f.Update(keyMsg("R"))
	if !m5.(Model).page2Running || cmd2 == nil {
		t.Fatal("R should rerun with the previous selection")
	}
	if got := m5.(Model).run2Domains; len(got) != 1 || got[0] != "probe.example" {
		t.Errorf("rerun domains = %v, want [probe.example]", got)
	}
}

func TestPage2SecondTabEnterDoesNotRestart(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	m2, _ := m.Update(keyMsg("tab"))
	mm := m2.(Model)
	mm.page2Running = false
	mm.page2EverRan = true
	mm.selActive = false // a real run would have closed the picker
	mm.page2Results, _ = fakePage2()

	m3, cmd2 := mm.Update(keyMsg("tab"))
	mm3 := m3.(Model)
	if mm3.tab != TabDNS {
		t.Fatalf("tab = %d", mm3.tab)
	}
	m4, cmd3 := mm3.Update(keyMsg("tab"))
	mm4 := m4.(Model)
	if mm4.page2Running || mm4.selActive || cmd3 != nil {
		t.Error("re-entering Page 2 with existing results must not re-trigger or reopen the picker")
	}
	_ = cmd2
}

func TestPageIndependentRunIDs(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	s1 := m.startProbe()
	s2 := s1.model.startResolve(s1.model.cfg.Domains)
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
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	s := m.startResolve(m.cfg.Domains)
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

func TestBothPagesRunConcurrently(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m1, cmd1 := m.Update(probeStartMsg{})
	m2, _ := m1.Update(keyMsg("tab"))
	// Confirming the picker starts Page 2 while Page 1 keeps running.
	m3, cmd2 := m2.Update(keyMsg("enter"))
	mm := m3.(Model)
	if !mm.page1Running || !mm.page2Running {
		t.Fatalf("combined mode: page1 running=%v, page2 running=%v, want both true", mm.page1Running, mm.page2Running)
	}

	d1, ok1 := cmd1().(probeDoneMsg)
	d2, ok2 := cmd2().(page2DoneMsg)
	if !ok1 || !ok2 {
		t.Fatalf("cmd types: %T / %T", cmd1(), cmd2())
	}
	m4, _ := mm.Update(d1)
	m5, _ := m4.Update(d2)
	f := m5.(Model)
	if f.page1Running || f.page2Running {
		t.Error("both pages should be finished")
	}
	if len(f.page1Results) == 0 {
		t.Error("page 1 must have results in combined mode")
	}
	if len(f.page2Results) != len(f.cfg.Domains) {
		t.Errorf("page 2 results = %d domains, want %d", len(f.page2Results), len(f.cfg.Domains))
	}
}

func TestPage2OnlyModeWithoutPage1(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m1, _ := m.Update(probeStartMsg{})
	m1s, _ := m1.Update(keyMsg("s"))
	stopped := m1s.(Model)
	if stopped.page1Running {
		t.Fatal("page 1 should be stopped")
	}

	m2, _ := stopped.Update(keyMsg("tab"))
	mm := m2.(Model)
	if !mm.selActive {
		t.Fatal("first Page 2 entry must show the domain picker")
	}
	m3, cmd2 := mm.Update(keyMsg("enter"))
	mm3 := m3.(Model)
	if !mm3.page2Running {
		t.Fatal("page 2 must run even though page 1 never completed (no dependency)")
	}
	d2, ok := cmd2().(page2DoneMsg)
	if !ok {
		t.Fatalf("cmd returned %T", cmd2())
	}
	m4, _ := mm3.Update(d2)
	f := m4.(Model)
	if len(f.page2Results) != len(f.cfg.Domains) {
		t.Errorf("page 2 results = %d domains, want %d", len(f.page2Results), len(f.cfg.Domains))
	}
}

func TestPage1OnlyModeNeverTriggersPage2(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m1, cmd1 := m.Update(probeStartMsg{})
	d1 := cmd1().(probeDoneMsg)
	m2, _ := m1.Update(d1)
	f := m2.(Model)
	if f.page2EverRan {
		t.Error("staying on Page 1 must never trigger the Page 2 test")
	}
	if len(f.page1Results) == 0 {
		t.Error("page 1 must have results")
	}
}

func TestRerunDoesNotDisturbOtherPage(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m1, _ := m.Update(probeStartMsg{})
	m2, _ := m1.Update(keyMsg("tab"))
	m3, _ := m2.Update(keyMsg("enter"))
	mm := m3.(Model)
	if !mm.page1Running || !mm.page2Running {
		t.Fatal("precondition: both pages running")
	}

	m4, cmd4 := mm.Update(keyMsg("R"))
	f := m4.(Model)
	if !f.page2Running || cmd4 == nil {
		t.Fatal("R should restart the running Page 2 test")
	}
	if f.run2ID != mm.run2ID+1 {
		t.Errorf("run2ID = %d, want %d", f.run2ID, mm.run2ID+1)
	}
	if !f.page1Running {
		t.Error("page 1 must keep running after Page 2 rerun")
	}
	if f.run1ID != mm.run1ID {
		t.Error("page 1 runID must be untouched")
	}
	_ = cmd4()
}

func TestNumberKeysSwitchTabs(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")

	m2, cmd := m.Update(keyMsg("2"))
	mm := m2.(Model)
	if mm.tab != TabGeo || !mm.selActive || mm.page2Running || cmd != nil {
		t.Error("key 2 should jump to Page 2 and open the domain picker on first entry")
	}
	m3, _ := mm.Update(keyMsg("1"))
	if m3.(Model).tab != TabDNS {
		t.Error("key 1 should jump back to Page 1")
	}
}

func TestStopDiscardsLateResults(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
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

	s2 := m.startResolve(m.cfg.Domains)
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

		// The domain picker must respect narrow terminals too.
		mp, _ := mm.Update(keyMsg("tab"))
		out2 := mp.(Model).View()
		for line := range strings.SplitSeq(out2, "\n") {
			if visible := visibleWidth(line); visible > size.Width {
				t.Errorf("picker line width %d exceeds terminal %d at resize: %q", visible, size.Width, line)
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

func TestViewPage1StoppedState(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, _ := s.model.Update(keyMsg("s"))
	stopped := m2.(Model)
	if stopped.page1Running || !stopped.page1Stopped {
		t.Fatalf("after S: running=%v stopped=%v, want false/true",
			stopped.page1Running, stopped.page1Stopped)
	}
	out := stripAnsi(stopped.View())
	for _, want := range []string{"已停止", "测试已停止，按 [S] 继续或 [R] 重新触发测试"} {
		if !strings.Contains(out, want) {
			t.Errorf("stopped Page 1 view missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"已完成", "未开始"} {
		if strings.Contains(out, banned) {
			t.Errorf("stopped Page 1 view must not show %q:\n%s", banned, out)
		}
	}

	// The placeholder promises that S resumes; it must really restart.
	m3, cmd := stopped.Update(keyMsg("s"))
	resumed := m3.(Model)
	if !resumed.page1Running || resumed.page1Stopped || cmd == nil {
		t.Errorf("S on a stopped probe should restart it (running=%v stopped=%v cmd=%v)",
			resumed.page1Running, resumed.page1Stopped, cmd != nil)
	}
}

func TestViewPage2StoppedState(t *testing.T) {
	m := New(testConfig())
	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	s := m.startResolve(m.cfg.Domains)
	s.model.tab = TabGeo
	m2, _ := s.model.Update(keyMsg("s"))
	stopped := m2.(Model)
	if stopped.page2Running || !stopped.page2Stopped {
		t.Fatalf("after S: running=%v stopped=%v, want false/true",
			stopped.page2Running, stopped.page2Stopped)
	}
	out := stripAnsi(stopped.View())
	for _, want := range []string{"已停止", "测试已停止，按 [S] 继续或 [R] 重新触发测试"} {
		if !strings.Contains(out, want) {
			t.Errorf("stopped Page 2 view missing %q:\n%s", want, out)
		}
	}
	for _, banned := range []string{"已完成", "未开始"} {
		if strings.Contains(out, banned) {
			t.Errorf("stopped Page 2 view must not show %q:\n%s", banned, out)
		}
	}

	m3, cmd := stopped.Update(keyMsg("s"))
	resumed := m3.(Model)
	if !resumed.page2Running || resumed.page2Stopped || cmd == nil {
		t.Errorf("S on a stopped resolve run should restart it (running=%v stopped=%v cmd=%v)",
			resumed.page2Running, resumed.page2Stopped, cmd != nil)
	}
}

func TestViewCompletedStateNotMisflagged(t *testing.T) {
	m := New(testConfig())
	s := m.startProbe()
	m2, _ := s.model.Update(probeDoneMsg{runID: s.model.run1ID, results: fakeResults()})
	done1 := m2.(Model)
	if done1.page1Running || done1.page1Stopped {
		t.Fatalf("completed Page 1: running=%v stopped=%v, want false/false",
			done1.page1Running, done1.page1Stopped)
	}
	out := stripAnsi(done1.View())
	if !strings.Contains(out, "已完成") {
		t.Errorf("completed Page 1 must show 已完成:\n%s", out)
	}
	if strings.Contains(out, "已停止") || strings.Contains(out, "未开始") {
		t.Errorf("completed Page 1 must not look stopped or not-started:\n%s", out)
	}

	m.geoClient = geo.NewClientWithEndpoint("http://127.0.0.1:1/json/")
	s2 := m.startResolve(m.cfg.Domains)
	s2.model.tab = TabGeo
	results, geoInfos := fakePage2()
	m3, _ := s2.model.Update(page2DoneMsg{runID: s2.model.run2ID, results: results, geo: geoInfos})
	done2 := m3.(Model)
	if done2.page2Running || done2.page2Stopped {
		t.Fatalf("completed Page 2: running=%v stopped=%v, want false/false",
			done2.page2Running, done2.page2Stopped)
	}
	out2 := stripAnsi(done2.View())
	if !strings.Contains(out2, "已完成") {
		t.Errorf("completed Page 2 must show 已完成:\n%s", out2)
	}
	if strings.Contains(out2, "已停止") || strings.Contains(out2, "未开始") {
		t.Errorf("completed Page 2 must not look stopped or not-started:\n%s", out2)
	}
}

func TestTableHeaderBoldStyle(t *testing.T) {
	headers := []string{"服务器", "协议", "RTT #1", "状态"}
	rows := [][]string{
		{"Mock One", "UDP", "12.0ms", "NOERROR"},
		{"Mock Two", "TCP", "40.0ms", "TIMEOUT"},
	}
	priority := []int{colProto, colStatus, colRTT1}

	// The default renderer degrades to the Ascii profile without a TTY,
	// which drops every SGR sequence; force one that keeps bold so the
	// assertion below is deterministic, then restore it.
	origProfile := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(origProfile)

	out := renderTable(headers, rows, nil, priority, 0)
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "\x1b[1m") {
		t.Errorf("table header row must carry the bold SGR sequence, got %q", lines[0])
	}
	if body := strings.Join(lines[1:], "\n"); strings.Contains(body, "\x1b[1m") {
		t.Errorf("data rows must not be wrapped in the bold style: %q", body)
	}

	// Styling must not change the header text or the column layout: a table
	// rendered without the bold style strips to exactly the same bytes.
	origStyle := headerCellStyle
	headerCellStyle = lipgloss.NewStyle()
	plain := renderTable(headers, rows, nil, priority, 0)
	headerCellStyle = origStyle
	if got, want := stripAnsi(out), stripAnsi(plain); got != want {
		t.Errorf("bold header changed the table layout:\ngot  %q\nwant %q", got, want)
	}
}

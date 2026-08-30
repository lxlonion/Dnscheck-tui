package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"dnscheck/internal/dnsclient"
)

// Column indexes in the page 1 table.
const (
	colName = iota
	colProto
	colRTT1
	colRTT2
	colRTT3
	colAvg
	colStatus
	colRate
)

// colPriority orders page 1 column inclusion when terminal width is scarce:
// status and success rate first, then average RTT, protocol, per-attempt RTTs.
var colPriority = []int{colStatus, colRate, colAvg, colProto, colRTT1, colRTT2, colRTT3}

// Column indexes in the page 2 table (服务器/协议/IP/位置/ISP).
const (
	colIP       = 2
	colLocation = 3
	colISP      = 4
)

type colSel struct {
	idx, w int
}

// View renders the whole UI: tab header, active page body, footer hints.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n\n")
	if m.tab == TabDNS {
		b.WriteString(m.page1View())
	} else if m.selActive {
		b.WriteString(m.selectorView())
	} else {
		b.WriteString(m.page2View())
	}
	b.WriteString("\n\n")
	b.WriteString(m.footerView())
	return b.String()
}

func (m Model) headerView() string {
	dns := "  [Tab / 1] DNS 性能测试"
	geo := "  [Tab / 2] 域名解析与 IP Geo"
	var line string
	if m.tab == TabDNS {
		line = tabActiveStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, dns, subtleStyle.Render(geo)))
	} else {
		line = subtleStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, dns, tabActiveStyle.Render(geo)))
	}
	return fitLine(line, m.width)
}

func (m Model) footerView() string {
	var hints string
	if m.selActive && m.tab == TabGeo {
		hints = "[↑/↓] 移动 | [空格] 勾选/取消 | [回车] 开始测试 | [Esc] 取消 | [Q] 退出程序"
		if m.width > 0 && m.width < 90 {
			hints = "[↑/↓] 移动 | [空格] 勾选 | [回车] 开始 | [Q] 退出"
		}
	} else {
		hints = "[Tab] 切换页面 | [R] 重新触发测试 | [S] 止/启测试"
		if m.tab == TabGeo && !m.page2Running {
			hints += " | [D] 选择域名"
		}
		hints += " | [Q] 退出程序"
		if m.width > 0 && m.width < 90 {
			hints = "[Tab] 切页 | [R] 重测 | [S] 止/启"
			if m.tab == TabGeo && !m.page2Running {
				hints += " | [D] 选域名"
			}
			hints += " | [Q] 退出"
		}
	}
	status := fmt.Sprintf("终端 %dx%d", m.width, m.height)
	line := subtleStyle.Render(hints) + "    " + subtleStyle.Render(status)
	if m.width > 0 {
		return fitLine(line, m.width)
	}
	return line
}

func (m Model) page1View() string {
	statusTxt := subtleStyle.Render("已完成")
	if m.page1Running {
		statusTxt = yellowStyle.Render("测试中…")
	} else if !m.page1EverRan {
		statusTxt = subtleStyle.Render("未开始")
	}
	title := titleStyle.Render("DNS 节点性能仪表盘")
	meta := fmt.Sprintf("探测域名: %s | Timeout: %s | ×%d | 状态: ",
		m.probeDomain(), m.cfg.Timeout(), probeAttempts) + statusTxt
	if m.width > 0 {
		meta = fitLine(meta, m.width)
	}

	headers := []string{"服务器", "协议", "RTT #1", "RTT #2", "RTT #3", "平均", "状态", "成功率"}
	rows := make([][]string, 0, len(m.page1Results))
	for _, pr := range m.page1Results {
		row := make([]string, len(headers))
		row[colName] = pr.Server.Name
		row[colProto] = strings.ToUpper(string(pr.Server.Protocol))
		for i := 0; i < probeAttempts; i++ {
			col := colRTT1 + i
			switch {
			case i >= len(pr.Attempts):
				row[col] = "—"
			case pr.Attempts[i].Success():
				row[col] = fmtRTT(pr.Attempts[i].RTT)
			default:
				row[col] = string(pr.Attempts[i].Status)
			}
		}
		if pr.Successes > 0 {
			row[colAvg] = fmtRTT(pr.AvgRTT)
		} else {
			row[colAvg] = "—"
		}
		row[colStatus] = aggregateStatus(pr.Attempts)
		row[colRate] = fmt.Sprintf("%.0f%% (%d/%d)", pr.SuccessRate()*100, pr.Successes, len(pr.Attempts))
		rows = append(rows, row)
	}

	decorators := make([]func(string) string, len(headers))
	decorators[colStatus] = func(s string) string {
		if s == string(dnsclient.StatusNOERROR) {
			return greenStyle.Render(s)
		}
		return redStyle.Render(s)
	}
	decorators[colRate] = func(s string) string {
		switch {
		case strings.HasPrefix(s, "100%"):
			return greenStyle.Render(s)
		case strings.HasPrefix(s, "0%"):
			return redStyle.Render(s)
		default:
			return yellowStyle.Render(s)
		}
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(meta)
	b.WriteString("\n\n")

	if len(m.page1Results) == 0 {
		if m.page1Running {
			b.WriteString(yellowStyle.Render("正在测试 DNS 服务器…"))
		} else {
			b.WriteString(subtleStyle.Render("暂无结果，按 [R] 重新触发测试"))
		}
		return b.String()
	}

	b.WriteString(renderTable(headers, rows, decorators, colPriority, m.width))
	return b.String()
}

// selectorView renders the Page 2 domain picker shown before the first
// resolve run (re-opened any time Page 2 is idle with [D]).
func (m Model) selectorView() string {
	checked := 0
	for _, c := range m.selChecked {
		if c {
			checked++
		}
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("域名解析与 IP Geo"))
	b.WriteString("\n")
	meta := subtleStyle.Render(fmt.Sprintf("选择要测试解析的域名（已选 %d / %d）",
		checked, len(m.cfg.Domains)))
	if m.width > 0 {
		meta = fitLine(meta, m.width)
	}
	b.WriteString(meta)
	b.WriteString("\n\n")

	if m.selWarn != "" {
		b.WriteString(yellowStyle.Render(m.selWarn))
		b.WriteString("\n\n")
	}

	for i, d := range m.cfg.Domains {
		cursor := "  "
		if i == m.selCursor {
			cursor = "> "
		}
		mark := subtleStyle.Render("[ ]")
		if i < len(m.selChecked) && m.selChecked[i] {
			mark = greenStyle.Render("[x]")
		}
		line := cursor + mark + " " + d
		if i == m.selCursor {
			line = cursorStyle.Render(line)
		}
		if m.width > 0 {
			line = fitLine(line, m.width)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) page2View() string {
	statusTxt := subtleStyle.Render("已完成")
	if m.page2Running {
		statusTxt = yellowStyle.Render("测试中…")
	} else if !m.page2EverRan {
		statusTxt = subtleStyle.Render("未开始")
	}
	title := titleStyle.Render("域名解析与 IP Geo")
	meta := subtleStyle.Render(fmt.Sprintf("Geo API: ip-api.com | 并发上限 %d | 内存缓存 | 状态: ", geoWorkers)) + statusTxt
	if m.width > 0 {
		meta = fitLine(meta, m.width)
	}

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(meta)
	b.WriteString("\n\n")

	if len(m.page2Results) == 0 {
		if m.page2Running {
			b.WriteString(yellowStyle.Render("正在解析域名并查询 IP 地理位置…"))
		} else {
			b.WriteString(subtleStyle.Render("暂无结果，按 [R] 重新触发测试"))
		}
		return b.String()
	}

	headers := []string{"服务器", "协议", "IP 地址", "位置", "ISP"}
	priority := []int{colIP, colLocation, colISP, colProto}
	for _, dr := range m.page2Results {
		b.WriteString(fmt.Sprintf("域名: %s\n", dr.Domain))
		rows := make([][]string, 0, len(dr.Entries))
		for _, e := range dr.Entries {
			ips := make([]string, 0, len(e.A)+len(e.AAAA))
			ips = append(ips, e.A...)
			ips = append(ips, e.AAAA...)
			if len(ips) == 0 {
				na := "N/A (No Answer)"
				if st := e.Status(); st != dnsclient.StatusNOERROR {
					na = "N/A (" + string(st) + ")"
				}
				rows = append(rows, []string{
					e.Server.Name,
					strings.ToUpper(string(e.Server.Protocol)),
					na, "", "",
				})
				continue
			}
			for _, ip := range ips {
				loc, isp := "-", "-"
				if g, ok := m.page2Geo[ip]; ok {
					if g.Err == nil {
						if l := g.Location(); l != "" {
							loc = l
						}
						if g.ISP != "" {
							isp = g.ISP
						}
					} else {
						loc = "Geo 查询失败"
					}
				}
				rows = append(rows, []string{
					e.Server.Name,
					strings.ToUpper(string(e.Server.Protocol)),
					ip, loc, isp,
				})
			}
		}
		b.WriteString(renderTable(headers, rows, nil, priority, m.width))
		b.WriteString("\n")
	}
	return b.String()
}

func aggregateStatus(attempts []dnsclient.Result) string {
	seen := make(map[string]bool)
	var order []string
	for _, a := range attempts {
		s := string(a.Status)
		if !seen[s] {
			seen[s] = true
			order = append(order, s)
		}
	}
	return strings.Join(order, "+")
}

func fmtRTT(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.2fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
}

func pad(s string, w int) string {
	gap := w - lipgloss.Width(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func truncateCell(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w-1 {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func fitLine(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

// renderTable lays out a column table that adapts to totalWidth: the name
// column is always present, other columns join in `priority` order while
// they fit. Cells are truncated to their column width.
func renderTable(headers []string, rows [][]string, decorators []func(string) string, priority []int, totalWidth int) string {
	ncol := len(headers)
	natural := make([]int, ncol)
	for i, h := range headers {
		natural[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if w := lipgloss.Width(c); w > natural[i] {
				natural[i] = w
			}
		}
	}
	nameCap := min(natural[colName]+1, 40)

	selected := []colSel{{colName, nameCap}}
	budget := max(nameCap, 10) + 2

	if totalWidth <= 0 {
		for i := 1; i < ncol; i++ {
			selected = append(selected, colSel{i, natural[i]})
			budget += natural[i] + 2
		}
	} else {
		for _, i := range priority {
			w := min(natural[i], 36)
			if budget+w+2 <= totalWidth {
				selected = append(selected, colSel{i, w})
				budget += w + 2
			}
		}
	}

	fixedOther := 0
	for _, c := range selected[1:] {
		fixedOther += c.w + 2
	}
	nameW := nameCap
	if totalWidth > 0 {
		nameW = clamp(totalWidth-fixedOther-2, 8, nameCap)
	}
	selected[0].w = nameW

	var sb strings.Builder
	sb.WriteString(renderRow(headers, selected, nil))
	sb.WriteString("\n")
	sepWidth := nameW + fixedOther
	if totalWidth > 0 && sepWidth > totalWidth {
		sepWidth = totalWidth
	}
	sb.WriteString(sepStyle.Render(strings.Repeat("─", max(sepWidth, 1))))
	sb.WriteString("\n")
	for _, row := range rows {
		sb.WriteString(renderRow(row, selected, decorators))
		sb.WriteString("\n")
	}
	return sb.String()
}

func renderRow(cells []string, selected []colSel, decorators []func(string) string) string {
	parts := make([]string, len(selected))
	for i, sel := range selected {
		c := truncateCell(cells[sel.idx], sel.w)
		if decorators != nil && decorators[sel.idx] != nil {
			c = decorators[sel.idx](c)
		}
		parts[i] = pad(c, sel.w)
	}
	return strings.Join(parts, "  ")
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

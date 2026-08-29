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

// colPriority orders column inclusion when terminal width is scarce:
// status and success rate first, then average RTT, protocol, per-attempt RTTs.
var colPriority = []int{colStatus, colRate, colAvg, colProto, colRTT1, colRTT2, colRTT3}

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
	} else {
		b.WriteString(m.geoPlaceholderView())
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
	hints := "[Tab] 切换页面 | [R] 重新触发测试 | [S] 止/启测试 | [Q] 退出程序"
	if m.width > 0 && m.width < 90 {
		hints = "[Tab] 切页 | [R] 重测 | [S] 止/启 | [Q] 退出"
	}
	status := fmt.Sprintf("终端 %dx%d", m.width, m.height)
	line := subtleStyle.Render(hints) + "    " + subtleStyle.Render(status)
	if m.width > 0 {
		return fitLine(line, m.width)
	}
	return line
}

func (m Model) geoPlaceholderView() string {
	domains := strings.Join(m.cfg.Domains, ", ")
	return titleStyle.Render("域名解析与 IP Geo") + "\n" +
		subtleStyle.Render("本页面将在后续阶段实现（域名列表: "+domains+"）")
}

func (m Model) page1View() string {
	statusTxt := subtleStyle.Render("已完成")
	if m.page1Running {
		statusTxt = yellowStyle.Render("测试中…")
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

	b.WriteString(renderTable(headers, rows, decorators, m.width))
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
// column is always present, other columns join in colPriority order while
// they fit. Cells are truncated to their column width.
func renderTable(headers []string, rows [][]string, decorators []func(string) string, totalWidth int) string {
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
		for _, i := range colPriority {
			w := min(natural[i], 24)
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

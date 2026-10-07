package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kzhuang/mini-golang-agent/internal/mcp"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

const mcpToolRows = 12

type mcpView struct {
	cursor int
	detail bool
	scroll int
	err    string
}

func (a *App) openMCP() tea.Cmd {
	a.mcpView = &mcpView{}
	a.view = viewMCP
	return nil
}

func (a *App) mcpServers() []mcp.ServerInfo {
	if a.mcp == nil {
		return nil
	}
	return a.mcp.Snapshot()
}

func (v *mcpView) update(a *App, msg tea.KeyMsg) tea.Cmd {
	servers := a.mcpServers()
	v.cursor = min(v.cursor, max(len(servers)-1, 0))
	key := msg.String()
	v.err = ""
	if v.detail {
		switch key {
		case "esc", "left", "q", "enter":
			v.detail = false
			return nil
		case "up", "k":
			v.scroll = max(v.scroll-1, 0)
			return nil
		case "down", "j":
			v.scroll++
			return nil
		}
	} else {
		switch key {
		case "esc", "q", "ctrl+c":
			a.closeOverlay()
			return nil
		case "up", "k":
			v.cursor = max(v.cursor-1, 0)
			return nil
		case "down", "j":
			v.cursor = min(v.cursor+1, max(len(servers)-1, 0))
			return nil
		case "enter", "right":
			if 0 < len(servers) {
				v.detail, v.scroll = true, 0
			}
			return nil
		}
	}
	if len(servers) == 0 {
		return nil
	}
	selected := servers[v.cursor]
	switch key {
	case "a":
		if selected.Status != mcp.StatusNeedsApproval {
			v.err = selected.Name + " does not need approval."
			return nil
		}
		if err := a.mcp.Approve(a.rt.BaseCtx, selected.Name); err != nil {
			v.err = err.Error()
		}
		return a.spin.Tick
	case "r":
		if err := a.mcp.Reconnect(a.rt.BaseCtx, selected.Name); err != nil {
			v.err = err.Error()
		}
		return a.spin.Tick
	}
	return nil
}

func (a *App) mcpStatus(s mcp.ServerInfo) string {
	switch s.Status {
	case mcp.StatusConnected:
		return styleOK.Render("● connected") + styleDim.Render(fmt.Sprintf(" · %d tools", len(s.Tools)))
	case mcp.StatusConnecting:
		return a.spin.View() + " connecting"
	case mcp.StatusNeedsApproval:
		return styleAccent.Render("⚠ needs approval")
	}
	return styleErr.Render("✗ failed")
}

func (v *mcpView) view(a *App) string {
	width := max(a.width-2, 10)
	servers := a.mcpServers()
	if v.detail && v.cursor < len(servers) {
		return stylePanel.Width(width).Render(v.detailView(a, servers[v.cursor]))
	}
	lines := []string{styleBold.Render("MCP servers"), ""}
	if a.mcp != nil {
		for _, err := range a.mcp.LoadErrors {
			lines = append(lines, styleErr.Render(clip(err.Error(), width-4)))
		}
	}
	if len(servers) == 0 {
		lines = append(lines,
			styleDim.Render("No MCP servers. Add one from the shell:"),
			"  mga mcp add <name> -- <command> [args...]",
			"  mga mcp add --transport http <name> <url>",
			styleDim.Render("or edit .mcp.json (project) or ~/.mga/mcp.json (user)."))
	}
	start, end := window(len(servers), v.cursor, pickerRows)
	for i := start; i < end; i++ {
		s := servers[i]
		row := fmt.Sprintf("%-18s %-7s %-5s ", clip(s.Name, 18), s.Scope, s.Transport) + a.mcpStatus(s) + styleDim.Render("  "+s.Target)
		lines = append(lines, cursorLine(i == v.cursor, clip(row, width-6)))
		if s.Status == mcp.StatusFailed {
			lines = append(lines, styleErr.Render(clip("     "+tools.OneLine(s.Err, 300), width-4)))
		}
	}
	if v.err != "" {
		lines = append(lines, "", styleErr.Render(v.err))
	}
	lines = append(lines, "", styleDim.Render("enter details · a approve · r reconnect · esc close"))
	return stylePanel.Width(width).Render(strings.Join(lines, "\n"))
}

func (v *mcpView) detailView(a *App, s mcp.ServerInfo) string {
	width := max(a.width-2, 10)
	lines := []string{
		styleBold.Render(s.Name) + styleDim.Render("  "+s.Scope+" · "+s.Transport) + "  " + a.mcpStatus(s),
		styleDim.Render("Target:  ") + clip(s.Target, width-14),
	}
	if s.ServerName != "" {
		lines = append(lines, styleDim.Render("Server:  ")+strings.TrimSpace(s.ServerName+" "+s.ServerVersion))
	}
	if s.Status == mcp.StatusNeedsApproval {
		lines = append(lines, "", styleAccent.Render("This server comes from the project's .mcp.json. mga starts it only after you approve it."),
			styleDim.Render("Check the command above, then press a to approve it for this directory."))
	}
	if s.Err != "" {
		lines = append(lines, "", styleErr.Render(clip("Error: "+tools.OneLine(s.Err, 500), width-4)))
	}
	if s.Stderr != "" && s.Status != mcp.StatusConnected {
		lines = append(lines, styleDim.Render("Server output (stderr):"), styleDim.Render(preview(s.Stderr, 6)))
	}
	if s.Instructions != "" {
		lines = append(lines, "", styleDim.Render("Instructions:"), preview(s.Instructions, 4))
	}
	if 0 < len(s.Tools) {
		lines = append(lines, "", styleDim.Render(fmt.Sprintf("Tools (%d):", len(s.Tools))))
		v.scroll = min(v.scroll, max(len(s.Tools)-mcpToolRows, 0))
		end := min(v.scroll+mcpToolRows, len(s.Tools))
		for _, t := range s.Tools[v.scroll:end] {
			tag := ""
			if t.ReadOnly {
				tag = styleInfo.Render(" read-only")
			}
			lines = append(lines, clip("  "+styleBold.Render(t.Name)+tag+styleDim.Render("  "+tools.OneLine(t.Description, 200)), width-4))
		}
		if end < len(s.Tools) {
			lines = append(lines, styleDim.Render(fmt.Sprintf("  … %d more (↓ to scroll)", len(s.Tools)-end)))
		}
	}
	if v.err != "" {
		lines = append(lines, "", styleErr.Render(v.err))
	}
	lines = append(lines, "", styleDim.Render("a approve · r reconnect · ↑/↓ scroll · esc back"))
	return strings.Join(lines, "\n")
}

func (a *App) reportMCP() {
	if a.mcpReported == nil {
		a.mcpReported = map[string]mcp.Status{}
	}
	for _, s := range a.mcpServers() {
		last, seen := a.mcpReported[s.Name]
		if seen && last == s.Status {
			continue
		}
		switch s.Status {
		case mcp.StatusFailed:
			a.emit(formatNote(styleErr.Render("MCP server "+s.Name+" failed: ") + tools.OneLine(s.Err, 160) + styleDim.Render(" · /mcp")))
		case mcp.StatusNeedsApproval:
			a.emit(formatNote("MCP server " + styleBold.Render(s.Name) + " from .mcp.json needs your approval" + styleDim.Render(" · /mcp")))
		case mcp.StatusConnected:
			if last == mcp.StatusFailed || last == mcp.StatusNeedsApproval {
				a.emit(formatNote(fmt.Sprintf("MCP server %s connected · %d tools", s.Name, len(s.Tools))))
			}
		case mcp.StatusConnecting:
			if seen {
				continue
			}
		}
		a.mcpReported[s.Name] = s.Status
	}
}

func displayToolName(name string) string {
	if server, tool, ok := mcp.SplitName(name); ok {
		return server + " - " + tool + " (MCP)"
	}
	return name
}

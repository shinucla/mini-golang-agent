package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kzhuang/mini-golang-agent/internal/agent"
	"github.com/kzhuang/mini-golang-agent/internal/tools"
)

var (
	colorAccent = lipgloss.Color("208")
	colorDim    = lipgloss.Color("244")
	colorGreen  = lipgloss.Color("42")
	colorRed    = lipgloss.Color("203")
	colorBlue   = lipgloss.Color("75")

	styleDim      = lipgloss.NewStyle().Foreground(colorDim)
	styleAccent   = lipgloss.NewStyle().Foreground(colorAccent)
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleErr      = lipgloss.NewStyle().Foreground(colorRed)
	styleOK       = lipgloss.NewStyle().Foreground(colorGreen)
	styleInfo     = lipgloss.NewStyle().Foreground(colorBlue)
	styleSelected = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleDone     = lipgloss.NewStyle().Foreground(colorDim).Strikethrough(true)
	styleBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorDim).Padding(0, 1)
	stylePanel    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(0, 1)
	styleUser     = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "237"}).
			Foreground(lipgloss.AdaptiveColor{Light: "235", Dark: "252"})
)

const toolPreviewLines = 6

type markdown struct {
	dark     bool
	width    int
	renderer *glamour.TermRenderer
}

func (m *markdown) render(text string, width int) string {
	if m.renderer == nil || m.width != width {
		cfg := styles.LightStyleConfig
		if m.dark {
			cfg = styles.DarkStyleConfig
		}
		zero := uint(0)
		cfg.Document.Margin = &zero
		cfg.Document.BlockPrefix = ""
		cfg.Document.BlockSuffix = ""
		r, err := glamour.NewTermRenderer(glamour.WithStyles(cfg), glamour.WithWordWrap(width))
		if err != nil {
			return text
		}
		m.renderer, m.width = r, width
	}
	out, err := m.renderer.Render(text)
	if err != nil {
		return text
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	for i, l := range lines {
		lines[i] = trimStyledSpaces(l)
	}
	return strings.Join(lines, "\n")
}

func indent(text, first, rest string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else {
			lines[i] = rest + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func clip(s string, width int) string {
	if width <= 1 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}

func formatUser(text string, width int) string {
	band := max(width-1, 12)
	body := strings.Split(indent(ansi.Wrap(text, band-3, ""), "> ", "  "), "\n")
	for i, l := range body {
		body[i] = styleUser.Render(l + strings.Repeat(" ", max(band-ansi.StringWidth(l), 1)))
	}
	return "\n" + strings.Join(body, "\n")
}

func formatAssistant(md *markdown, text string, width int) string {
	body := md.render(text, max(width-2, 20))
	return "\n" + indent(body, "⏺ ", "  ")
}

func formatError(msg string) string {
	return styleErr.Render("  ⎿  " + msg)
}

func formatNote(msg string) string {
	return "\n" + styleInfo.Render("⏺ ") + msg
}

func formatToolHeader(name, summary string, ok bool, width int) string {
	bullet := styleOK.Render("⏺")
	if !ok {
		bullet = styleErr.Render("⏺")
	}
	head := styleBold.Render(displayToolName(name))
	if summary != "" {
		head += "(" + summary + ")"
	}
	return bullet + " " + clip(head, width-2)
}

func formatToolBlock(name, summary, args, result string, isErr bool, width int) string {
	body := toolBody(name, args, result, isErr)
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		lines[i] = clip(strings.ReplaceAll(l, "\t", "  "), width-6)
	}
	return "\n" + formatToolHeader(name, summary, !isErr, width) + "\n" + indent(strings.Join(lines, "\n"), "  ⎿  ", "     ")
}

func toolBody(name, args, result string, isErr bool) string {
	result = strings.TrimRight(result, "\n")
	if isErr {
		return styleErr.Render(preview(result, toolPreviewLines))
	}
	switch name {
	case "Read":
		return fmt.Sprintf("Read %d lines", strings.Count(result, "\n")+1)
	case "Edit":
		var in tools.EditInput
		if json.Unmarshal([]byte(args), &in) == nil {
			return result + "\n" + diffPreview(in.OldString, in.NewString)
		}
	case "Glob":
		if result == "No files found" {
			return result
		}
		return fmt.Sprintf("Found %d files", strings.Count(result, "\n")+1)
	case "TodoWrite":
		_, list, _ := strings.Cut(result, "\n")
		return list
	case "WebFetch":
		return fmt.Sprintf("Fetched %d characters", len(result))
	}
	if result == "" {
		return styleDim.Render("(no output)")
	}
	return preview(result, toolPreviewLines)
}

func preview(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	return strings.Join(lines[:n], "\n") + "\n" + styleDim.Render(fmt.Sprintf("… +%d lines", len(lines)-n))
}

func diffPreview(oldText, newText string) string {
	const limit = 8
	var out []string
	add := func(prefix string, text string, style lipgloss.Style) {
		if text == "" {
			return
		}
		lines := strings.Split(text, "\n")
		for i, l := range lines {
			if i == limit {
				out = append(out, styleDim.Render(fmt.Sprintf("  … +%d lines", len(lines)-limit)))
				break
			}
			out = append(out, style.Render(prefix+l))
		}
	}
	add("- ", oldText, styleErr)
	add("+ ", newText, styleOK)
	return strings.Join(out, "\n")
}

func formatTodos(todos []tools.Todo) string {
	var lines []string
	for _, t := range todos {
		switch t.Status {
		case tools.TodoCompleted:
			lines = append(lines, styleDone.Render("☒ "+t.Content))
		case tools.TodoInProgress:
			lines = append(lines, styleBold.Render("◐ "+t.Content))
		default:
			lines = append(lines, "☐ "+t.Content)
		}
	}
	return strings.Join(lines, "\n")
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func window(n, cursor, size int) (int, int) {
	if n <= size {
		return 0, n
	}
	start := min(max(cursor-size/2, 0), n-size)
	return start, start + size
}

func cursorLine(selected bool, text string) string {
	if selected {
		return styleSelected.Render("❯ ") + text
	}
	return "  " + text
}

func approvalPreview(req agent.ApprovalRequest) []string {
	var text string
	switch req.Tool {
	case "Edit":
		var in tools.EditInput
		if json.Unmarshal(req.Input, &in) != nil {
			return nil
		}
		text = diffPreview(in.OldString, in.NewString)
	case "Write":
		var in struct {
			Content string `json:"content"`
		}
		if json.Unmarshal(req.Input, &in) != nil {
			return nil
		}
		text = diffPreview("", in.Content)
	default:
		return nil
	}
	return append([]string{""}, strings.Split(indent(text, "  ", "  "), "\n")...)
}

func titledBox(content, title string, width int) string {
	box := styleBox.Width(width).Render(content)
	if title == "" {
		return box
	}
	body := styleBox.BorderTop(false).Width(width).Render(content)
	outer := lipgloss.Width(strings.SplitN(body, "\n", 2)[0])
	room := outer - 7
	if room < 1 {
		return box
	}
	title = clip(strings.Join(strings.Fields(title), " "), room)
	dashes := outer - lipgloss.Width(title) - 5
	top := styleDim.Render("╭"+strings.Repeat("─", dashes)+" ") + title + styleDim.Render(" ─╮")
	return top + "\n" + body
}

func wrappedRows(runes []rune, width int) int {
	if width <= 0 {
		return 1
	}
	rows, lineWidth, lineUsed, spaces := 1, 0, false, 0
	var word []rune
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		wordWidth := ansi.StringWidth(string(word))
		if 0 < spaces {
			if width < lineWidth+wordWidth+spaces {
				rows++
				lineWidth = wordWidth + spaces
			} else {
				lineWidth += wordWidth + spaces
			}
			lineUsed = true
			spaces, word = 0, nil
			continue
		}
		if width < wordWidth+ansi.StringWidth(string(word[len(word)-1])) {
			if lineUsed {
				rows++
			}
			lineWidth, lineUsed, word = wordWidth, true, nil
		}
	}
	if width <= lineWidth+ansi.StringWidth(string(word))+spaces {
		rows++
	}
	return rows
}

var styledTrailingSpace = regexp.MustCompile(`(\x1b\[[0-9;]*m| )+$`)

func trimStyledSpaces(line string) string {
	trimmed := styledTrailingSpace.ReplaceAllString(line, "")
	if trimmed == line {
		return line
	}
	return trimmed + "\x1b[0m"
}

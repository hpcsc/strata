package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent = lipgloss.Color("212")
	dim    = lipgloss.Color("240")

	titleFocused  = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accent).Bold(true)
	titleBlurred  = lipgloss.NewStyle().Foreground(dim).Bold(true)
	borderFocused = lipgloss.NewStyle().Foreground(accent)
	borderBlurred = lipgloss.NewStyle().Foreground(dim)
	dimText       = lipgloss.NewStyle().Foreground(dim)
	boldText      = lipgloss.NewStyle().Bold(true)
	selectedText  = lipgloss.NewStyle().Foreground(accent).Bold(true)
	currentText   = lipgloss.NewStyle().Foreground(lipgloss.Color("114")).Bold(true)
	warningText   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorText     = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	addedText     = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	deletedText   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	renamedText   = lipgloss.NewStyle().Foreground(lipgloss.Color("110"))
	modifiedText  = lipgloss.NewStyle().Foreground(lipgloss.Color("179"))
	viewedText    = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
)

// box draws a panel exactly width by height cells, with the title set into
// the top border.
func box(title string, lines []string, width, height int, focused bool) string {
	if width < 4 || height < 2 {
		return ""
	}
	inner := width - 2
	horizontal, vertical := "─", "│"
	topLeft, topRight, bottomLeft, bottomRight := "╭", "╮", "╰", "╯"
	border, titleStyle := borderBlurred, titleBlurred
	if focused {
		horizontal, vertical = "━", "┃"
		topLeft, topRight, bottomLeft, bottomRight = "┏", "┓", "┗", "┛"
		border, titleStyle = borderFocused, titleFocused
	}
	chip := titleStyle.Render(" " + ansi.Truncate(title, max(0, inner-3), "…") + " ")
	rows := make([]string, 0, height)
	rows = append(rows, border.Render(topLeft+horizontal)+chip+
		border.Render(strings.Repeat(horizontal, max(0, inner-1-lipgloss.Width(chip)))+topRight))
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		rows = append(rows, border.Render(vertical)+fit(line, inner)+border.Render(vertical))
	}
	rows = append(rows, border.Render(bottomLeft+strings.Repeat(horizontal, inner)+bottomRight))
	return strings.Join(rows, "\n")
}

func fit(line string, width int) string {
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width, "…")
	}
	return line + "\x1b[0m" + strings.Repeat(" ", width-ansi.StringWidth(line))
}

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

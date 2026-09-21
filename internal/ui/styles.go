// Package ui holds the shared Lip Gloss theme for the fakerforge CLI.
//
// Design rule: styled output is TTY-only. Every helper checks Enabled()
// first and returns plain text when stdout is piped, NO_COLOR is set,
// TERM=dumb, or --no-color was passed. This keeps --format json,
// --no-progress, and CI logs byte-for-byte plain.
package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

var (
	accent = lipgloss.Color("#7C9AFF")
	green  = lipgloss.Color("#04B575")
	red    = lipgloss.Color("#FF5F56")
	yellow = lipgloss.Color("#E3B341")
	mutedC = lipgloss.Color("#8B8B93")
)

// Styles — constructed once, reused everywhere.
var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	WarnStyle = lipgloss.NewStyle().
			Foreground(yellow).
			Bold(true)

	MutedStyle = lipgloss.NewStyle().
			Foreground(mutedC)

	BoldStyle = lipgloss.NewStyle().
			Bold(true)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedC).
			Padding(0, 1)

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(accent)

	PillReadyStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	PillPendingStyle = lipgloss.NewStyle().
				Foreground(yellow).
				Bold(true)

	PillFailedStyle = lipgloss.NewStyle().
			Foreground(red).
			Bold(true)

	BarFilledStyle = lipgloss.NewStyle().
			Foreground(green).
			Bold(true)

	BarEmptyStyle = lipgloss.NewStyle().
			Foreground(mutedC)

	BarPercentStyle = lipgloss.NewStyle().
			Bold(true)
)

// forcePlain is set by --no-color (see SetNoColor). Env-based gating
// (NO_COLOR / TERM=dumb) is evaluated on every call so tests that
// setenv mid-process behave correctly.
var forcePlain bool

// SetNoColor disables all styling for the rest of the process.
// Called from flag parsing when --no-color is passed.
func SetNoColor(v bool) {
	forcePlain = v
	lipgloss.SetColorProfile(termenv.Ascii)
}

// NoColor reports whether styling must be suppressed.
func NoColor() bool {
	if forcePlain {
		return true
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	return false
}

// IsTTY reports whether stdout is an interactive terminal.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Enabled reports whether styled output should be used.
func Enabled() bool {
	return IsTTY() && !NoColor()
}

// Title renders a section heading.
func Title(s string) string {
	if !Enabled() {
		return s
	}
	return TitleStyle.Render(s)
}

// Success renders a success line (e.g. "✓ Connected").
func Success(s string) string {
	if !Enabled() {
		return s
	}
	return SuccessStyle.Render(s)
}

// Error renders an error line.
func Error(s string) string {
	if !Enabled() {
		return s
	}
	return ErrorStyle.Render(s)
}

// Warn renders a warning line.
func Warn(s string) string {
	if !Enabled() {
		return s
	}
	return WarnStyle.Render(s)
}

// Muted renders de-emphasized text (hints, secondary info).
func Muted(s string) string {
	if !Enabled() {
		return s
	}
	return MutedStyle.Render(s)
}

// Bold renders emphasized text.
func Bold(s string) string {
	if !Enabled() {
		return s
	}
	return BoldStyle.Render(s)
}

// Box wraps content in a rounded border (TTY only).
func Box(s string) string {
	if !Enabled() {
		return s
	}
	return BoxStyle.Render(s)
}

// Dot returns a colored status dot: green for ok, red for failure.
func Dot(ok bool) string {
	if !Enabled() {
		if ok {
			return "OK"
		}
		return "FAIL"
	}
	if ok {
		return SuccessStyle.Render("●")
	}
	return ErrorStyle.Render("●")
}

// StatusPill colorizes ready/pending/failed style statuses.
// Unknown statuses pass through untouched (plain when disabled).
func StatusPill(status string) string {
	if !Enabled() {
		return status
	}
	switch strings.ToLower(status) {
	case "ready", "ok", "success":
		return PillReadyStyle.Render(status)
	case "pending", "generating", "parsing", "relationships", "queued":
		return PillPendingStyle.Render(status)
	case "failed", "fail", "error":
		return PillFailedStyle.Render(status)
	default:
		return status
	}
}

// ansiRe matches SGR escape sequences so column widths are computed on
// visible characters, not styling bytes. Cells may arrive pre-styled
// (e.g. StatusPill), so measuring raw length would misalign the table.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visibleLen returns the printable width of s, ignoring ANSI codes.
func visibleLen(s string) int {
	return len(ansiRe.ReplaceAllString(s, ""))
}

// Table renders headers + rows as an aligned table with a styled header
// on TTY and plain aligned columns when piped. Widths are computed on
// plain text so alignment holds either way.
func Table(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = visibleLen(h)
	}
	for _, r := range rows {
		for i := range headers {
			if i < len(r) && visibleLen(r[i]) > widths[i] {
				widths[i] = visibleLen(r[i])
			}
		}
	}
	pad := func(s string, w int) string {
		if d := w - visibleLen(s); d > 0 {
			return s + strings.Repeat(" ", d)
		}
		return s
	}

	var b strings.Builder
	// Header.
	var line strings.Builder
	for i, h := range headers {
		if i > 0 {
			line.WriteString("  ")
		}
		cell := pad(h, widths[i])
		if Enabled() {
			cell = HeaderStyle.Render(cell)
		}
		line.WriteString(cell)
	}
	b.WriteString(strings.TrimRight(line.String(), " "))
	b.WriteString("\n")
	// Rows.
	for _, r := range rows {
		line.Reset()
		for i := range headers {
			if i > 0 {
				line.WriteString("  ")
			}
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			line.WriteString(pad(cell, widths[i]))
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ProgressBar returns e.g. "[######--------------]". On TTY the filled
// portion is green and the empty portion muted; when piped it falls back
// to the plain ASCII bar so logs stay clean. gen is clamped to [0, req];
// req <= 0 yields an indeterminate empty bar.
func ProgressBar(gen, req, width int) string {
	if width < 5 {
		width = 5
	}
	if req <= 0 {
		bar := "[" + strings.Repeat("-", width) + "]"
		if !Enabled() {
			return bar
		}
		return BarEmptyStyle.Render(bar)
	}
	if gen < 0 {
		gen = 0
	}
	if gen > req {
		gen = req
	}
	filled := gen * width / req
	if !Enabled() {
		return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
	}
	return "[" +
		BarFilledStyle.Render(strings.Repeat("#", filled)) +
		BarEmptyStyle.Render(strings.Repeat("-", width-filled)) +
		"]"
}

// Percent returns e.g. " 42%" or "--%" when req <= 0.
func Percent(gen, req int) string {
	if req <= 0 {
		return "--%"
	}
	if gen < 0 {
		gen = 0
	}
	pct := gen * 100 / req
	if pct > 100 {
		pct = 100
	}
	s := fmt.Sprintf("%3d%%", pct)
	if !Enabled() {
		return s
	}
	return BarPercentStyle.Render(s)
}

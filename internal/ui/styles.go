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
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// Brand palette, matched to the FakerForge web app (forge orange + ember).
var (
	accent = lipgloss.Color("#FF750F") // forge orange (primary)
	amber  = lipgloss.Color("#FFB020") // gradient tail
	green  = lipgloss.Color("#2FBF71")
	red    = lipgloss.Color("#FF5F56")
	yellow = lipgloss.Color("#E3B341")
	mutedC = lipgloss.Color("#8B8B93")
	lineC  = lipgloss.Color("#6C6C75")
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
			BorderForeground(lineC).
			Padding(0, 1)

	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lineC).
			Padding(0, 2)

	BorderStyle = lipgloss.NewStyle().
			Foreground(lineC)

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
			Foreground(accent).
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

// Enabled reports whether styled output should be used. FORCE_COLOR /
// CLICOLOR_FORCE force styling on (handy for demos and screenshots);
// NO_COLOR and --no-color always win.
func Enabled() bool {
	if NoColor() {
		return false
	}
	if forcedColor() {
		return true
	}
	return IsTTY()
}

func forcedColor() bool {
	for _, k := range []string{"FORCE_COLOR", "CLICOLOR_FORCE"} {
		if v, ok := os.LookupEnv(k); ok && v != "" && v != "0" {
			return true
		}
	}
	return false
}

// ── brand ────────────────────────────────────────────────────────────────────

// wordmark is "FAKERFORGE" in a compact 5-row block font (letter width 5).
var wordmark = func() []string {
	glyphs := map[rune][]string{
		'F': {"█████", "█    ", "████ ", "█    ", "█    "},
		'A': {" ███ ", "█   █", "█████", "█   █", "█   █"},
		'K': {"█   █", "█  █ ", "███  ", "█  █ ", "█   █"},
		'E': {"█████", "█    ", "████ ", "█    ", "█████"},
		'R': {"████ ", "█   █", "████ ", "█  █ ", "█   █"},
		'O': {" ███ ", "█   █", "█   █", "█   █", " ███ "},
		'G': {" ███ ", "█    ", "█  ██", "█   █", " ███ "},
	}
	letters := "FAKERFORGE"
	rows := make([]string, 5)
	for row := 0; row < 5; row++ {
		parts := make([]string, 0, len(letters))
		for _, r := range letters {
			parts = append(parts, glyphs[r][row])
		}
		rows[row] = strings.Join(parts, " ")
	}
	return rows
}()

// Banner returns the multi-line FakerForge wordmark with a tagline, or ""
// when styling is disabled (callers can safely print it unconditionally).
func Banner() string {
	if !Enabled() {
		return ""
	}
	var b strings.Builder
	for _, line := range wordmark {
		b.WriteString(gradientLine(line, string(accent), string(amber)))
		b.WriteString("\n")
	}
	b.WriteString("  ")
	b.WriteString(MutedStyle.Render("Forge realistic fake data straight into your database"))
	b.WriteString("\n")
	return b.String()
}

// Brand returns the compact one-line brand mark, or "" when disabled.
func Brand() string {
	if !Enabled() {
		return ""
	}
	return TitleStyle.Render("◆ FakerForge")
}

// gradientLine colors each non-space rune along a from→to hex gradient.
func gradientLine(s, from, to string) string {
	runes := []rune(s)
	n := len(runes)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(lerpHex(from, to, t)))
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

func lerpHex(a, b string, t float64) string {
	ar, ag, ab := hexRGB(a)
	br, bg, bb := hexRGB(b)
	r := ar + int(float64(br-ar)*t)
	g := ag + int(float64(bg-ag)*t)
	bl := ab + int(float64(bb-ab)*t)
	return fmt.Sprintf("#%02X%02X%02X", clamp8(r), clamp8(g), clamp8(bl))
}

func hexRGB(h string) (int, int, int) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 0, 0, 0
	}
	r, _ := strconv.ParseInt(h[0:2], 16, 32)
	g, _ := strconv.ParseInt(h[2:4], 16, 32)
	b, _ := strconv.ParseInt(h[4:6], 16, 32)
	return int(r), int(g), int(b)
}

func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// ── primitives ───────────────────────────────────────────────────────────────

// Title renders a section heading.
func Title(s string) string {
	if !Enabled() {
		return s
	}
	return TitleStyle.Render(s)
}

// Section renders a heading with a leading diamond (TTY only).
func Section(s string) string {
	if !Enabled() {
		return s
	}
	return TitleStyle.Render("◆ " + s)
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

// Panel wraps content in a rounded panel with an optional pre-styled title.
// Callers style the title (e.g. Success("✓ Logged in")) so its color is kept.
func Panel(title, body string) string {
	if !Enabled() {
		if title != "" {
			return title + "\n" + body
		}
		return body
	}
	content := body
	if title != "" {
		content = title + "\n\n" + body
	}
	return PanelStyle.Render(content)
}

// KV renders aligned key/value pairs: muted keys, bold values on TTY;
// "key: value" lines when piped.
func KV(pairs [][2]string) string {
	width := 0
	for _, p := range pairs {
		if len(p[0]) > width {
			width = len(p[0])
		}
	}
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteString("\n")
		}
		if !Enabled() {
			b.WriteString(p[0] + ": " + p[1])
			continue
		}
		key := p[0] + strings.Repeat(" ", width-len(p[0]))
		b.WriteString(MutedStyle.Render(key))
		b.WriteString("   ")
		b.WriteString(p[1])
	}
	return b.String()
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

// Table renders headers + rows. On a TTY it draws a rounded box with a
// header rule; when piped it falls back to the plain aligned columns so
// logs stay byte-for-byte clean.
func Table(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}
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

	if !Enabled() {
		var b strings.Builder
		var line strings.Builder
		for i, h := range headers {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(pad(h, widths[i]))
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
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

	seg := func(w int) string { return strings.Repeat("─", w+2) }
	var top, mid, bottom strings.Builder
	top.WriteString("╭")
	mid.WriteString("├")
	bottom.WriteString("╰")
	for i, w := range widths {
		if i > 0 {
			top.WriteString("┬")
			mid.WriteString("┼")
			bottom.WriteString("┴")
		}
		top.WriteString(seg(w))
		mid.WriteString(seg(w))
		bottom.WriteString(seg(w))
	}
	top.WriteString("╮")
	mid.WriteString("┤")
	bottom.WriteString("╯")

	// On a narrow terminal a bordered grid would wrap and look broken;
	// fall back to a stacked card per row instead.
	total := 1
	for _, w := range widths {
		total += w + 3
	}
	if tw, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && tw > 0 && total > tw {
		return cardTable(headers, rows)
	}

	border := func(s string) string { return BorderStyle.Render(s) }

	var b strings.Builder
	b.WriteString(border(top.String()))
	b.WriteString("\n")
	b.WriteString(border("│"))
	for i, h := range headers {
		b.WriteString(HeaderStyle.Render(" " + pad(h, widths[i]) + " "))
		b.WriteString(border("│"))
	}
	b.WriteString("\n")
	b.WriteString(border(mid.String()))
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString(border("│"))
		for i := range headers {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			b.WriteString(" " + pad(cell, widths[i]) + " ")
			b.WriteString(border("│"))
		}
		b.WriteString("\n")
	}
	b.WriteString(border(bottom.String()))
	return b.String()
}

// cardTable renders each row as a small labelled card, used when a bordered
// grid would not fit the terminal. The first column becomes the card title.
func cardTable(headers []string, rows [][]string) string {
	labelWidth := 0
	for i := 1; i < len(headers); i++ {
		if len(headers[i]) > labelWidth {
			labelWidth = len(headers[i])
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		if ri > 0 {
			b.WriteString("\n")
		}
		title := ""
		if len(r) > 0 {
			title = r[0]
		}
		b.WriteString(HeaderStyle.Render("◆ " + title))
		b.WriteString("\n")
		for i := 1; i < len(headers); i++ {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			label := headers[i]
			if d := labelWidth - len(label); d > 0 {
				label += strings.Repeat(" ", d)
			}
			b.WriteString("  " + MutedStyle.Render(label) + "  " + cell + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ProgressBar returns e.g. "[######--------------]". On TTY the filled
// portion is the brand orange and the empty portion muted; when piped it
// falls back to the plain ASCII bar so logs stay clean. gen is clamped to
// [0, req]; req <= 0 yields an indeterminate empty bar.
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

package output

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bspiritxp/jcemb/internal/app/embed"
	"golang.org/x/term"
)

const (
	progressBarWidth            = 30
	progressDefaultTerminalCols = 100
	progressMaxPathWidth        = 40
	progressMinPathWidth        = 8
)

type terminalWidthFunc func(io.Writer) int

type EmbedProgressBar struct {
	mu            sync.Mutex
	writer        io.Writer
	terminalWidth terminalWidthFunc
	total         int
	completed     int
	current       string
	status        string
}

func NewEmbedProgressBar(writer io.Writer) *EmbedProgressBar {
	return &EmbedProgressBar{writer: writer, terminalWidth: detectTerminalWidth}
}

func (p *EmbedProgressBar) Update(update embed.ProgressUpdate) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.total = update.Total
	p.completed = update.Completed
	p.current = update.Current
	p.status = update.Status

	if p.total == 0 {
		return
	}

	percent := float64(p.completed) / float64(p.total)
	filled := int(percent * float64(progressBarWidth))
	if filled > progressBarWidth {
		filled = progressBarWidth
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)

	statusIcon := "⚡"
	switch p.status {
	case "skipped":
		statusIcon = "⏭️"
	case "error":
		statusIcon = Colorize(Red, "✗")
	}
	plainStatusIcon := stripANSIEscapes(statusIcon)
	pathWidth := progressPathWidth(p.terminalWidth(p.writer), p.completed, p.total, plainStatusIcon)

	line := fmt.Sprintf("\r\033[K%s [%s] %s%d%s/%d %s %s",
		Colorize(Cyan, "Scanning"),
		Colorize(Green, bar),
		Colorize(Bold, ""),
		p.completed,
		Reset,
		p.total,
		statusIcon,
		Colorize(Dim, truncatePath(p.current, pathWidth)),
	)

	_, _ = p.writer.Write([]byte(line))
}

func (p *EmbedProgressBar) Finish(summary embed.Summary) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, _ = p.writer.Write([]byte("\n\n"))

	_, _ = fmt.Fprintf(p.writer, "%s %s\n\n", Colorize(Green, "✓"), Boldf("Scan complete!"))
	_, _ = fmt.Fprintf(p.writer, "  %s  Processed:  %s\n", Colorize(Cyan, "▸"), Colorize(White, fmt.Sprintf("%d", summary.Processed)))
	_, _ = fmt.Fprintf(p.writer, "  %s  Updated:    %s\n", Colorize(Yellow, "↻"), Colorize(White, fmt.Sprintf("%d", summary.Updated)))
	_, _ = fmt.Fprintf(p.writer, "  %s  Skipped:    %s\n", Colorize(Gray, "⏭"), Colorize(White, fmt.Sprintf("%d", summary.Skipped)))
	if summary.Deleted > 0 {
		_, _ = fmt.Fprintf(p.writer, "  %s  Deleted:    %s\n", Colorize(Red, "🗑"), Colorize(White, fmt.Sprintf("%d", summary.Deleted)))
	}
	if summary.Errors > 0 {
		_, _ = fmt.Fprintf(p.writer, "  %s  Errors:     %s\n", Colorize(Red, "✗"), Colorize(White, fmt.Sprintf("%d", summary.Errors)))
	}
	_, _ = p.writer.Write([]byte("\n"))
}

func truncatePath(path string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if displayWidth(path) <= maxLen {
		return path
	}
	if maxLen <= 3 {
		return strings.Repeat(".", maxLen)
	}

	runes := []rune(path)
	suffixWidth := maxLen - 3
	width := 0
	start := len(runes)
	for start > 0 {
		nextWidth := runeDisplayWidth(runes[start-1])
		if width+nextWidth > suffixWidth {
			break
		}
		width += nextWidth
		start--
	}
	return "..." + string(runes[start:])
}

func progressPathWidth(columns, completed, total int, statusIcon string) int {
	if columns <= 0 {
		columns = progressDefaultTerminalCols
	}

	counter := fmt.Sprintf("%d/%d", completed, total)
	fixedWidth := displayWidth("Scanning ") +
		displayWidth("[") + progressBarWidth + displayWidth("] ") +
		displayWidth(counter) + displayWidth(" ") +
		displayWidth(statusIcon) + displayWidth(" ")

	available := columns - fixedWidth - 1
	if available > progressMaxPathWidth {
		return progressMaxPathWidth
	}
	if available < progressMinPathWidth {
		return progressMinPathWidth
	}
	return available
}

type fdWriter interface {
	Fd() uintptr
}

func detectTerminalWidth(writer io.Writer) int {
	fd, ok := writer.(fdWriter)
	if !ok {
		return progressDefaultTerminalCols
	}

	fileDescriptor := int(fd.Fd())
	if !term.IsTerminal(fileDescriptor) {
		return progressDefaultTerminalCols
	}

	width, _, err := term.GetSize(fileDescriptor)
	if err != nil || width <= 0 {
		return progressDefaultTerminalCols
	}
	return width
}

func stripANSIEscapes(value string) string {
	var builder strings.Builder
	inEscape := false
	for _, r := range value {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\033' {
			inEscape = true
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func displayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	case r < 32 || (r >= 0x7f && r < 0xa0):
		return 0
	case r >= 0x0300 && r <= 0x036f:
		return 0
	case r >= 0xfe00 && r <= 0xfe0f:
		return 0
	case r >= 0x1100 && r <= 0x115f:
		return 2
	case r >= 0x2e80 && r <= 0xa4cf:
		return 2
	case r >= 0xac00 && r <= 0xd7a3:
		return 2
	case r >= 0xf900 && r <= 0xfaff:
		return 2
	case r >= 0xfe10 && r <= 0xfe19:
		return 2
	case r >= 0xfe30 && r <= 0xfe6f:
		return 2
	case r >= 0xff00 && r <= 0xff60:
		return 2
	case r >= 0xffe0 && r <= 0xffe6:
		return 2
	case r >= 0x1f300 && r <= 0x1faff:
		return 2
	default:
		return 1
	}
}

package output

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bspiritxp/jcemb/internal/app/embed"
)

const progressBarWidth = 30

type EmbedProgressBar struct {
	mu        sync.Mutex
	writer    io.Writer
	total     int
	completed int
	current   string
	status    string
}

func NewEmbedProgressBar(writer io.Writer) *EmbedProgressBar {
	return &EmbedProgressBar{writer: writer}
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

	line := fmt.Sprintf("\r\033[K%s [%s] %s%d%s/%d %s %s",
		Colorize(Cyan, "Embedding"),
		Colorize(Green, bar),
		Colorize(Bold, ""),
		p.completed,
		Reset,
		p.total,
		statusIcon,
		Colorize(Dim, truncatePath(p.current, 40)),
	)

	_, _ = p.writer.Write([]byte(line))
}

func (p *EmbedProgressBar) Finish(summary embed.Summary) {
	p.mu.Lock()
	defer p.mu.Unlock()

	_, _ = p.writer.Write([]byte("\n\n"))

	_, _ = fmt.Fprintf(p.writer, "%s %s\n\n", Colorize(Green, "✓"), Boldf("Embedding complete!"))
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
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

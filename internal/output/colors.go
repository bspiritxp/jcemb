package output

import "fmt"

const (
	Reset   = "\033[0m"
	Bold    = "\033[1m"
	Dim     = "\033[2m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Gray    = "\033[90m"
)

func Colorize(color, text string) string {
	return color + text + Reset
}

func ColorForScore(score float64) string {
	switch {
	case score >= 0.5:
		return Green
	case score >= 0.35:
		return Yellow
	default:
		return Gray
	}
}

func ScoreLabel(score float64) string {
	switch {
	case score >= 0.5:
		return Colorize(Green, "high")
	case score >= 0.35:
		return Colorize(Yellow, "med")
	default:
		return Colorize(Gray, "low")
	}
}

func Boldf(format string, args ...any) string {
	return Bold + fmt.Sprintf(format, args...) + Reset
}

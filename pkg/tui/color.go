package tui

import "fmt"

const (
	// Normal
	Black   = "\033[30m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	Reset = "\033[0m"

	// Bright
	BrightBlack   = "\033[90m"
	BrightRed     = "\033[91m"
	BrightGreen   = "\033[92m"
	BrightYellow  = "\033[93m"
	BrightBlue    = "\033[94m"
	BrightMagenta = "\033[95m"
	BrightCyan    = "\033[96m"
	BrightWhite   = "\033[97m"

	// Style
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Underline = "\033[4m"
	Blink     = "\033[5m"
	Reverse   = "\033[7m"

	//Background
	BgBlack   = "\033[40m"
	BgRed     = "\033[41m"
	BgGreen   = "\033[42m"
	BgYellow  = "\033[43m"
	BgBlue    = "\033[44m"
	BgMagenta = "\033[45m"
	BgCyan    = "\033[46m"
	BgWhite   = "\033[47m"
)

func ColorizeNumber(v uint64) string {
	switch {
	case v > 80:
		return BrightRed + fmt.Sprintf("%d", v) + Reset
	case v > 50:
		return Yellow + fmt.Sprintf("%d", v) + Reset
	default:
		return BrightGreen + fmt.Sprintf("%d", v) + Reset
	}
}

func ColorizeTraffic(v float64) string {
	switch {
	case v > 1000:
		return BrightRed + fmt.Sprintf("%.0f", v) + Reset
	case v > 100:
		return Yellow + fmt.Sprintf("%.0f", v) + Reset
	default:
		return BrightBlack + fmt.Sprintf("%.0f", v) + Reset
	}
}

func ColorizeError(v uint64) string {
	switch {
	case v > 1:
		return BrightRed + fmt.Sprintf("%d", v) + Reset
	default:
		return BrightBlack + fmt.Sprintf("%d", v) + Reset
	}
}

func ColorizeString(s string, color string) string {
	return color + s + Reset
}
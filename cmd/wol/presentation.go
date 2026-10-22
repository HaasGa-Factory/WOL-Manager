package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type colorMode uint8

const (
	colorNone colorMode = iota
	colorANSI16
	colorANSI256
	colorTrueColor
)

type messageStyle uint8

const (
	styleWarning messageStyle = iota
	styleError
	styleSuccess
	styleInfo
	styleTitle
	styleSection
	styleCommand
	styleDescription
	styleMuted
)

// Reset for every CLI invocation by run. It keeps presentation flags out of
// the Wake-on-LAN, NUT, network and API implementations.
var cliNoColor bool

func colorEnabled(output io.Writer) bool {
	return outputColorMode(output) != colorNone
}

func outputColorMode(output io.Writer) colorMode {
	file, ok := output.(*os.File)
	if !ok {
		return colorNone
	}
	info, err := file.Stat()
	if err != nil {
		return colorNone
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	return detectColorMode(info.Mode()&os.ModeCharDevice != 0, noColor || cliNoColor, os.Getenv("TERM"), os.Getenv("COLORTERM"))
}

func allowColor(tty, noColor bool, term string) bool {
	return detectColorMode(tty, noColor, term, "") != colorNone
}

func detectColorMode(tty, disabled bool, term, colorTerm string) colorMode {
	if !tty || disabled || strings.EqualFold(term, "dumb") {
		return colorNone
	}
	// The environment is only a capability hint after the TTY check. An
	// absent TERM gets conservative ANSI 16-color output, useful over SSH from
	// Windows consoles that do not always forward TERM.
	if strings.EqualFold(colorTerm, "truecolor") || strings.EqualFold(colorTerm, "24bit") {
		return colorTrueColor
	}
	if strings.Contains(strings.ToLower(term), "256color") {
		return colorANSI256
	}
	return colorANSI16
}

func styledText(text string, style messageStyle, enabled bool) string {
	mode := colorNone
	if enabled {
		mode = colorANSI16
	}
	return styledTextMode(text, style, mode)
}

func styledTextMode(text string, style messageStyle, mode colorMode) string {
	if mode == colorNone || text == "" {
		return text
	}
	return "\x1b[" + styleCode(style, mode) + "m" + text + "\x1b[0m"
}

func styleCode(style messageStyle, mode colorMode) string {
	if mode == colorTrueColor {
		switch style {
		case styleWarning, styleSection:
			return "1;38;2;251;191;36"
		case styleError:
			return "1;38;2;248;113;113"
		case styleSuccess, styleCommand:
			return "38;2;74;222;128"
		case styleInfo:
			return "38;2;96;165;250"
		case styleTitle:
			return "1;38;2;34;211;238"
		case styleDescription:
			return "38;2;209;213;219"
		case styleMuted:
			return "38;2;156;163;175"
		}
	}
	if mode == colorANSI256 {
		switch style {
		case styleWarning, styleSection:
			return "1;38;5;214"
		case styleError:
			return "1;38;5;203"
		case styleSuccess, styleCommand:
			return "38;5;78"
		case styleInfo:
			return "38;5;75"
		case styleTitle:
			return "1;38;5;45"
		case styleDescription:
			return "38;5;252"
		case styleMuted:
			return "38;5;246"
		}
	}
	switch style {
	case styleWarning, styleSection:
		return "1;33"
	case styleError:
		return "1;31"
	case styleSuccess, styleCommand:
		return "32"
	case styleInfo:
		return "34"
	case styleTitle:
		return "1;36"
	case styleDescription:
		return "37"
	case styleMuted:
		return "90"
	default:
		return "0"
	}
}

func messagef(output io.Writer, style messageStyle, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	fmt.Fprintln(output, styledTextMode(text, style, outputColorMode(output)))
}

func warningf(output io.Writer, format string, args ...any) {
	messagef(output, styleWarning, format, args...)
}

func errorf(output io.Writer, format string, args ...any) {
	messagef(output, styleError, format, args...)
}

func successf(output io.Writer, format string, args ...any) {
	messagef(output, styleSuccess, format, args...)
}

func infof(output io.Writer, format string, args ...any) {
	messagef(output, styleInfo, format, args...)
}

func outputWidth(output io.Writer) int {
	if file, ok := output.(*os.File); ok {
		if width := terminalWidth(file); width > 0 {
			return clampWidth(width)
		}
	}
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width > 0 {
		return clampWidth(width)
	}
	return 80
}

func clampWidth(width int) int {
	if width < 40 {
		return 40
	}
	if width > 240 {
		return 240
	}
	return width
}

func visibleWidth(text string) int {
	width := 0
	inEscape := false
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		text = text[size:]
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	if r == 0 || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r == '⚡' ||
		(r >= 0x1100 && r <= 0x115f) ||
		(r >= 0x2e80 && r <= 0xa4cf) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0x1f300 && r <= 0x1faff) ||
		(r >= 0x20000 && r <= 0x3fffd) {
		return 2
	}
	return 1
}

func wrapText(text string, width int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	if width < 1 {
		width = 1
	}
	words := strings.Fields(text)
	lines := make([]string, 0, 1)
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if visibleWidth(line)+1+visibleWidth(word) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

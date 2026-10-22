package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type messageStyle string

const (
	styleWarning messageStyle = "1;33"
	styleError   messageStyle = "1;31"
	styleSuccess messageStyle = "1;32"
	styleInfo    messageStyle = "1;36"
)

func colorEnabled(output io.Writer) bool {
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	_, noColor := os.LookupEnv("NO_COLOR")
	return err == nil && allowColor(info.Mode()&os.ModeCharDevice != 0, noColor, os.Getenv("TERM"))
}

func allowColor(tty, noColor bool, term string) bool {
	return tty && !noColor && term != "" && !strings.EqualFold(term, "dumb")
}

func styledText(text string, style messageStyle, enabled bool) string {
	if !enabled {
		return text
	}
	return "\x1b[" + string(style) + "m" + text + "\x1b[0m"
}

func messagef(output io.Writer, style messageStyle, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	fmt.Fprintln(output, styledText(text, style, colorEnabled(output)))
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

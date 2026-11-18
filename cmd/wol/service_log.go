package main

import (
	"fmt"
	"io"
	"os"
)

type serviceLogFunc func(level, format string, args ...any)

func processServiceLog(level, format string, args ...any) {
	writeServiceLog(os.Stderr, os.Getenv("JOURNAL_STREAM") != "", level, format, args...)
}

func writeServiceLog(output io.Writer, journal bool, level, format string, args ...any) {
	prefix := ""
	if journal {
		prefix = map[string]string{"INFO": "<6>", "WARNING": "<4>", "ERROR": "<3>"}[level]
	}
	fmt.Fprintf(output, "%s%s %s\n", prefix, level, fmt.Sprintf(format, args...))
}

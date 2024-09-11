package util

import (
	"log/slog"
)

type LogWriter struct{}

func (w LogWriter) Write(b []byte) (n int, err error) {
	n = len(b)
	if n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	slog.Info(string(b))
	return n, nil
}

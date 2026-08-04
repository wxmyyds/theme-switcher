package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Logger struct {
	mu      sync.Mutex
	file    *os.File
	enabled bool
}

func New(path string, enabled bool) *Logger {
	if !enabled {
		return &Logger{enabled: false}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		backup := filepath.Join(os.TempDir(), "theme_switcher.log")
		file, err = os.OpenFile(backup, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return &Logger{enabled: false}
		}
	}
	return &Logger{file: file, enabled: true}
}

func (l *Logger) Log(format string, args ...interface{}) {
	if !l.enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(l.file, "[%s] ", timestamp)
	fmt.Fprintf(l.file, format, args...)
	fmt.Fprintln(l.file)
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}
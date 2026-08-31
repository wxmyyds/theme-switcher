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
	// warn 保存打开阶段产生的告警（如日志轮转失败），在首次 Log 时输出。
	// 打开阶段还没有可用的输出目标，只能延迟写入。
	warn error
}

const maxLogSize = 1 << 20

// openRotated 打开日志文件，超过大小上限时先轮转。
// 轮转失败不当作致命错误：仍然返回可写的文件句柄（日志会继续增长），
// 并把原因通过 warn 返回，避免异常被静默吞掉。
func openRotated(path string) (f *os.File, warn error, err error) {
	if fi, statErr := os.Stat(path); statErr == nil && fi.Size() > maxLogSize {
		oldPath := path + ".old"
		if rmErr := os.Remove(oldPath); rmErr != nil && !os.IsNotExist(rmErr) {
			warn = fmt.Errorf("删除旧日志 %s 失败: %w", oldPath, rmErr)
		}
		if rnErr := os.Rename(path, oldPath); rnErr != nil {
			if warn == nil {
				warn = fmt.Errorf("轮转日志 %s 失败，日志将继续增长: %w", path, rnErr)
			}
		}
	}
	f, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	return f, warn, err
}

func New(path string, enabled bool) *Logger {
	if !enabled {
		return &Logger{enabled: false}
	}
	file, warn, err := openRotated(path)
	if err == nil {
		return &Logger{file: file, enabled: true, warn: warn}
	}

	// 同目录不可写时退到临时目录，保证故障时可诊断。
	backup := filepath.Join(os.TempDir(), "theme_switcher.log")
	file, warn2, err := openRotated(backup)
	if err != nil {
		return &Logger{enabled: false}
	}
	l := &Logger{file: file, enabled: true}
	l.note("日志路径 %s 不可写，已回退到 %s", path, backup)
	if warn2 != nil {
		l.note("%v", warn2)
	}
	return l
}

// note 记录第一条告警，后续告警丢弃（避免刷屏）。
func (l *Logger) note(format string, args ...interface{}) {
	if l.warn == nil {
		l.warn = fmt.Errorf(format, args...)
	}
}

func (l *Logger) Log(format string, args ...interface{}) {
	if !l.enabled {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if l.warn != nil {
		fmt.Fprintf(l.file, "[%s] 警告: %v\n", timestamp, l.warn)
		l.warn = nil
	}
	fmt.Fprintf(l.file, "[%s] ", timestamp)
	fmt.Fprintf(l.file, format, args...)
	fmt.Fprintln(l.file)
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

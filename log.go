//go:build linux

package infratrail

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	LevelDebug uint8 = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)
const (
	DEBUG = " [debug] "
	INFO  = " [info] "
	WARN  = " [warn] "
	ERROR = "  [error] "
	FATAL = "  [fatal] "
)

var bufferPool = sync.Pool{New: func() any { return new([]byte) }}

func getBuffer() *[]byte {
	p := bufferPool.Get().(*[]byte)
	*p = (*p)[:0]
	return p
}

func putBuffer(p *[]byte) {
	if cap(*p) > 64<<10 {
		*p = nil
	}
	bufferPool.Put(p)
}

type Logger struct {
	out   io.Writer // *os.File
	outMu sync.Mutex
	Chan  struct {
		cacheLen uint64
		channel  chan *[]byte
		used     bool
	}
	pc       uintptr
	disLevel uint8
	trace    bool
}

func (l *Logger) CloseChan() {
	defer recover()
	close(l.Chan.channel)
}
func (l *Logger) UseChan() {
	if l.Chan.cacheLen == 0 {
		l.Chan.cacheLen = 1 << 7
	}
	l.Chan.channel = make(chan *[]byte, l.Chan.cacheLen)
	l.Chan.used = true
	go func() {
		for {
			m, opened := <-l.Chan.channel
			if !opened {
				return
			}
			l.writeMessage(m)
		}
	}()
}
func (l *Logger) SetCacheSize(n uint64) {
	if !l.Chan.used {
		return
	}
	l.Chan.cacheLen = n
}
func (l *Logger) SetDisplayLevel(level uint8) {
	l.disLevel = level
}
func (l *Logger) SetTraceEnabled() {
	l.trace = true
}
func (l *Logger) SetPC(pc uintptr) {
	l.pc = pc
}
func cutFPath3(fp string) string {

	fps := strings.Split(fp, "/")
	fpsl := len(fps)
	if fpsl >= 3 {
		return strings.Join(fps[fpsl-3:fpsl], "/")
	}
	return fp
}

// error、fatal打印堆栈
func (l *Logger) output(pc uintptr, calldepth int, level string, appendOutput func([]byte) []byte) error {
	prefix := time.Now().Local().Format(time.DateTime + ".000 ")
	prefix = prefix + level // timestamp + level
	var (
		file string
		line int
	)

	buf := getBuffer()
	defer putBuffer(buf)

	formatHeader(buf, prefix)
	*buf = appendOutput(*buf)
	if !l.trace {
		goto END
	}

	if pc == 0 {
		var ok bool
		_, file, line, ok = runtime.Caller(calldepth)
		if !ok {
			file = "???"
			line = 0
		} else {
			file = cutFPath3(file)
		}

		formatCaller(buf, file, line)
	} else {
		pcs := make([]uintptr, pc)
		n := runtime.Callers(calldepth+1, pcs)
		frames := runtime.CallersFrames(pcs[:n])
		*buf = append(*buf, "\n[trace dump]"...)
		for i := 0; i < int(pc); i++ {
			frame, more := frames.Next()
			file = cutFPath3(frame.File)
			*buf = append(*buf, fmt.Sprintf("\n[%#v]::%s::%s:%d", frame.PC, filepath.Base(frame.Function), file, frame.Line)...)
			if !more {
				break
			}
		}
	}

END: // add a line break
	if len(*buf) == 0 || (*buf)[len(*buf)-1] != '\n' {
		*buf = append(*buf, '\n')
	}

	if !l.Chan.used {
		return l.writeMessage(buf)
	}
	l.Chan.channel <- buf
	return nil
}

func (l *Logger) writeMessage(m *[]byte) error {
	if !l.Chan.used {
		l.outMu.Lock()
		defer l.outMu.Unlock()
	}
	_, err := l.out.Write(*m)
	return err
}
func formatHeader(buf *[]byte, prefix string) {
	*buf = append(*buf, prefix...)
}
func formatCaller(buf *[]byte, file string, line int) {
	*buf = append(*buf, fmt.Sprintf(" - Caller: %s:%d", file, line)...)
}

func (l *Logger) Debugf(format string, v ...any) {
	if l.disLevel > LevelDebug {
		return
	}

	l.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func (l *Logger) Debug(v ...any) {
	if l.disLevel > LevelDebug {
		return
	}

	l.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func (l *Logger) Infof(format string, v ...any) {
	if l.disLevel > LevelInfo {
		return
	}
	l.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func (l *Logger) Info(v ...any) {
	if l.disLevel > LevelInfo {
		return
	}
	l.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func (l *Logger) Warnf(format string, v ...any) {
	if l.disLevel > LevelWarn {
		return
	}
	l.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func (l *Logger) Warn(v ...any) {
	if l.disLevel > LevelWarn {
		return
	}
	l.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func (l *Logger) Errorf(format string, v ...any) {
	if l.disLevel > LevelError {
		return
	}

	l.output(l.pc, 2, ERROR, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func (l *Logger) Error(v ...any) {
	if l.disLevel > LevelError {
		return
	}
	l.output(l.pc, 2, ERROR, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func (l *Logger) Fatalf(format string, v ...any) {
	if l.disLevel > LevelFatal {
		return
	}
	l.output(l.pc, 2, FATAL, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
	os.Exit(125)
}
func (l *Logger) Fatal(v ...any) {
	if l.disLevel > LevelFatal {
		return
	}
	l.output(l.pc, 2, FATAL, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
	os.Exit(125)
}

func New(out io.Writer) *Logger {
	return &Logger{
		out:      out,
		disLevel: LevelInfo,
	}
}

var std = New(os.Stderr)

func Debugf(format string, v ...any) {
	if std.disLevel > LevelDebug {
		return
	}

	std.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func Debug(v ...any) {
	if std.disLevel > LevelDebug {
		return
	}

	std.output(0, 2, DEBUG, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func Infof(format string, v ...any) {
	if std.disLevel > LevelInfo {
		return
	}

	std.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func Info(v ...any) {
	if std.disLevel > LevelInfo {
		return
	}

	std.output(0, 2, INFO, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func Warnf(format string, v ...any) {
	if std.disLevel > LevelWarn {
		return
	}

	std.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func Warn(v ...any) {
	if std.disLevel > LevelWarn {
		return
	}

	std.output(0, 2, WARN, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func Errorf(format string, v ...any) {
	if std.disLevel > LevelError {
		return
	}

	std.output(std.pc, 2, ERROR, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
}
func Error(v ...any) {
	if std.disLevel > LevelError {
		return
	}

	std.output(std.pc, 2, ERROR, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
}
func Fatalf(format string, v ...any) {
	if std.disLevel > LevelFatal {
		return
	}

	std.output(std.pc, 2, FATAL, func(b []byte) []byte {
		return fmt.Appendf(b, format, v...)
	})
	os.Exit(125)
}
func Fatal(v ...any) {
	if std.disLevel > LevelFatal {
		return
	}

	std.output(std.pc, 2, FATAL, func(b []byte) []byte {
		return fmt.Append(b, v...)
	})
	os.Exit(125)
}

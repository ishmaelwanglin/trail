//go:build linux

package trail

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
	LevelNone uint8 = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

const (
	NONE  = ""
	DEBUG = "debug"
	INFO  = "info"
	WARN  = "warn"
	ERROR = "error"
	FATAL = "fatal"
)

// line format
const (
	TXT uint8 = iota
	JSON
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

var muMulti sync.Mutex // 多个logger同时打开一个文件
func shareLock() {
	muMulti.Lock()
}
func shareUnLock() {
	muMulti.Unlock()
}

type Logger struct {
	out   io.Writer // *os.File
	outMu sync.Mutex
	Chan  struct {
		cacheLen uint64
		channel  chan *[]byte
		used     bool
	}
	pc          uintptr
	calldepth   int
	disLevel    uint8
	trace       bool
	format      uint8
	staticLevel uint8 // 如果有值，这个logger就是固定的level
}

func (l *Logger) SetOutput(out io.Writer) *Logger {
	l.out = out

	return l
}

func (l *Logger) SetFormat(format uint8) error {
	if format > 1 {
		return fmt.Errorf("invalid format")
	}
	l.format = format
	return nil
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
func (l *Logger) output(pc uintptr, calldepth int, level string, message func() string) error {

	timeStr := time.Now().Local().Format(time.DateTime + ".000")

	buf := getBuffer()
	defer putBuffer(buf)

	switch l.format {
	case JSON:
		*buf = append(*buf, fmt.Sprintf(`{"time":"%s","level":"%s","message":"`, timeStr, level)...)
	case TXT:
		*buf = append(*buf, fmt.Sprintf("%s [%s] ", timeStr, level)...)
	}

	*buf = append(*buf, message()...)

	var (
		file string
		line int
	)

	if l.trace {
		if l.format == JSON {
			pc = 0
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
			switch l.format {
			case JSON:
				*buf = append(*buf, fmt.Sprintf(`","caller":"%s:%d"}`, file, line)...)
			case TXT:
				*buf = append(*buf, fmt.Sprintf(` - Caller: %s:%d`, file, line)...)
			}
		} else {
			// txt格式
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
	}

	// add a line break
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
	if l.staticLevel > 0 {
		shareLock()
		defer shareUnLock()
	}
	if !l.Chan.used {
		l.outMu.Lock()
		defer l.outMu.Unlock()
	}
	_, err := l.out.Write(*m)
	return err
}

func (l *Logger) Debugf(format string, v ...any) {
	if l.disLevel > LevelDebug {
		return
	}
	l.output(0, l.calldepth, DEBUG, func() string {
		return fmt.Sprintf(format, v...)
	})
}

func (l *Logger) Debug(v ...any) {
	if l.disLevel > LevelDebug {
		return
	}
	l.output(0, l.calldepth, DEBUG, func() string {
		return fmt.Sprint(v...)
	})
}

func (l *Logger) Infof(format string, v ...any) {
	if l.disLevel > LevelInfo {
		return
	}
	l.output(0, l.calldepth, INFO, func() string {
		return fmt.Sprintf(format, v...)
	})
}

func (l *Logger) Info(v ...any) {
	if l.disLevel > LevelInfo {
		return
	}
	l.output(0, l.calldepth, INFO, func() string {
		return fmt.Sprint(v...)
	})
}

func (l *Logger) Warnf(format string, v ...any) {
	if l.disLevel > LevelWarn {
		return
	}
	l.output(0, l.calldepth, WARN, func() string {
		return fmt.Sprintf(format, v...)
	})
}

func (l *Logger) Warn(v ...any) {
	if l.disLevel > LevelWarn {
		return
	}
	l.output(0, l.calldepth, WARN, func() string {
		return fmt.Sprint(v...)
	})
}

func (l *Logger) Errorf(format string, v ...any) {
	if l.disLevel > LevelError {
		return
	}
	l.output(l.pc, l.calldepth, ERROR, func() string {
		return fmt.Sprintf(format, v...)
	})
}

func (l *Logger) Error(v ...any) {
	if l.disLevel > LevelError {
		return
	}
	l.output(l.pc, l.calldepth, ERROR, func() string {
		return fmt.Sprint(v...)
	})
}

func (l *Logger) Fatalf(format string, v ...any) {
	if l.disLevel > LevelFatal {
		return
	}
	l.output(l.pc, l.calldepth, FATAL, func() string {
		return fmt.Sprintf(format, v...)
	})
	os.Exit(1)
}

func (l *Logger) Fatal(v ...any) {
	if l.disLevel > LevelFatal {
		return
	}
	l.output(l.pc, l.calldepth, FATAL, func() string {
		return fmt.Sprint(v...)
	})
	os.Exit(1)
}

func levelD2S(l uint8) string {
	switch l {
	case LevelNone:
		return NONE
	case LevelDebug:
		return DEBUG
	case LevelInfo:
		return INFO
	case LevelWarn:
		return WARN
	case LevelError:
		return ERROR
	case LevelFatal:
		return FATAL
	default:
		return NONE
	}
}
func (l *Logger) Println(v ...any) {
	l.output(l.pc, l.calldepth, levelD2S(l.staticLevel), func() string {
		return fmt.Sprint(v...)
	})
}

func (l *Logger) Printf(format string, v ...any) {
	l.output(l.pc, l.calldepth, levelD2S(l.staticLevel), func() string {
		return fmt.Sprintf(format, v...)
	})
}

func (l *Logger) Writer() io.Writer {
	return l.out
}

func (l *Logger) SetStaticLevel(level uint8) *Logger {
	l.staticLevel = level
	return l
}

func New() *Logger {
	return &Logger{
		out:       os.Stderr,
		disLevel:  LevelInfo,
		calldepth: 2,
	}
}

var std = &Logger{
	out:       os.Stderr,
	disLevel:  LevelInfo,
	calldepth: 3,
}

func SetOutput(out io.Writer) {
	std.SetOutput(out)
}

func SetTraceEnabled() {
	std.SetTraceEnabled()
}

func SetDisplayLevel(level uint8) {
	std.disLevel = level
}

func SetPC(pc uintptr) {
	std.SetPC(pc)
}

func SetFormat(format uint8) {
	std.SetFormat(format)
}

func Debugf(format string, v ...any) {
	std.Debugf(format, v...)
}

func Debug(v ...any) {
	std.Debug(v...)
}

func Infof(format string, v ...any) {
	std.Infof(format, v...)
}

func Info(v ...any) {
	std.Info(v...)
}

func Warnf(format string, v ...any) {
	std.Warnf(format, v...)
}

func Warn(v ...any) {
	std.Warn(v...)
}

func Errorf(format string, v ...any) {
	std.Errorf(format, v...)
}

func Error(v ...any) {
	std.Error(v...)
}

func Fatalf(format string, v ...any) {
	std.Fatalf(format, v...)
}

func Fatal(v ...any) {
	std.Fatal(v...)
}

func Println(v ...any) {
	std.Println(v...)
}

func Printf(format string, v ...any) {
	std.Printf(format, v...)
}

func Writer() io.Writer {
	return std.out
}

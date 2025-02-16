package trail

import (
	"bytes"
	"encoding/json"
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

var levelStrings = [...]string{
	LevelNone:  "",
	LevelDebug: "debug",
	LevelInfo:  "info",
	LevelWarn:  "warn",
	LevelError: "error",
	LevelFatal: "fatal",
}

// line format
const (
	FormatText uint8 = iota
	FormatJson
)

var bufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func getBuffer() *bytes.Buffer {
	p := bufferPool.Get().(*bytes.Buffer)
	// *p = (*p)[:0]
	p.Reset()
	return p
}

func putBuffer(p *bytes.Buffer) {
	// if cap(p.Cap) > 64<<10 {
	// 	*p = nil
	// }
	bufferPool.Put(p)
}

var muMulti sync.Mutex // 多个logger同时打开一个文件

type Logger struct {
	mu    sync.Mutex
	out   io.Writer // *os.File
	async struct {
		qlen    uint64
		queue   chan *bytes.Buffer
		enabled bool
	}
	pc            uintptr
	callDepth     int
	levelNormal   uint8 // display level
	caller        bool
	format        uint8
	levelPriority uint8 // 如果有值，这个logger就是固定的level,适配mqtt的自带日志logger,并且拿锁使用共享锁
}

func (l *Logger) shareLock()   { muMulti.Lock() }
func (l *Logger) shareUnlock() { muMulti.Unlock() }

func (l *Logger) CloseQueue() {
	defer recover()
	close(l.async.queue)
}
func (l *Logger) Async() {
	if l.async.qlen == 0 {
		l.async.qlen = 1 << 7
	}
	l.async.queue = make(chan *bytes.Buffer, l.async.qlen)
	l.async.enabled = true
	go func() {
		for {
			m, opened := <-l.async.queue
			if !opened {
				return
			}
			l.writeMessage(m)
		}
	}()
}

func (l *Logger) SetCalldepth(n int) *Logger {
	l.callDepth = n
	return l
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
func (l *Logger) SetCacheSize(n uint64) {
	if !l.async.enabled {
		return
	}
	l.async.qlen = n
}
func (l *Logger) SetlevelNormal(level uint8) *Logger {
	l.levelNormal = level
	return l
}
func (l *Logger) SetCaller(on bool) *Logger {
	l.caller = on
	return l
}
func (l *Logger) SetPC(pc uintptr) *Logger {
	l.pc = pc
	return l
}

func cutFilePath3(fp string) string {
	fps := strings.Split(fp, "/")
	fpsl := len(fps)
	if fpsl >= 3 {
		return strings.Join(fps[fpsl-3:fpsl], "/")
	}
	return fp
}

var once sync.Once

type jsonLog struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Caller  string `json:"caller,omitempty"`
}

func (l *Logger) getCaller(level uint8) *bytes.Buffer {
	var b bytes.Buffer
	if !l.caller {
		return &b
	}
	pc := l.pc
	if l.format == FormatJson || level < LevelError {
		pc = 0
	}
	if pc == 0 {
		_, file, line, ok := runtime.Caller(l.callDepth)
		if !ok {
			file = "???"
			line = 0
		} else {
			file = cutFilePath3(file)
		}
		b.WriteString(fmt.Sprintf("%s:%d", file, line))
	} else {
		pcs := make([]uintptr, l.pc)
		n := runtime.Callers(l.callDepth+1, pcs)
		frames := runtime.CallersFrames(pcs[:n])
		b.WriteString("\n[trace dump]")
		for i := 0; i < int(l.pc); i++ {
			frame, more := frames.Next()
			b.WriteString(fmt.Sprintf("\n[%#v]::%s::%s:%d", frame.PC, filepath.Base(frame.Function), cutFilePath3(frame.File), frame.Line))
			if !more {
				break
			}
		}
	}
	return &b
}

func (l *Logger) output(level uint8, message func() string) error {
	if l.levelPriority > l.levelNormal {
		once.Do(func() { l.levelNormal = l.levelPriority })
	}
	if level < l.levelNormal {
		return nil
	}
	timeStr := time.Now().Local().Format(time.DateTime + ".000")
	buf := getBuffer()
	defer putBuffer(buf)
	switch l.format {
	case FormatJson:
		j := jsonLog{
			Time:    timeStr,
			Level:   levelStrings[level],
			Message: message(),
		}
		if l.caller {
			j.Caller = l.getCaller(level).String()
		}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(j); err != nil {
			buf.WriteString(fmt.Sprintf(`{"error":"%v"}`, err))
		}
	case FormatText:
		buf.WriteString(fmt.Sprintf("%s %-7s %s", timeStr, fmt.Sprintf("[%s]", levelStrings[level]), message()))
		if l.caller {
			caller := l.getCaller(level).String()
			if caller != "" {
				buf.WriteString(fmt.Sprintf(" - Caller: %s", caller))
			}
		}
	}
	// add a line break
	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		buf.WriteString("\n")
	}

	if !l.async.enabled {
		return l.writeMessage(buf)
	}
	l.async.queue <- buf
	return nil
}

func (l *Logger) writeMessage(b *bytes.Buffer) error {
	if l.async.enabled {
		goto WRITE
	}

	if l.levelPriority > LevelNone {
		l.shareLock()
		defer l.shareUnlock()
	} else {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

WRITE:
	_, err := l.out.Write(b.Bytes())
	return err
}

func (l *Logger) Debugf(format string, v ...any) {
	l.output(LevelDebug, func() string { return fmt.Sprintf(format, v...) })
}
func (l *Logger) Debug(v ...any) {
	l.output(LevelDebug, func() string { return fmt.Sprint(v...) })
}
func (l *Logger) Infof(format string, v ...any) {
	l.output(LevelInfo, func() string { return fmt.Sprintf(format, v...) })
}
func (l *Logger) Info(v ...any) {
	l.output(LevelInfo, func() string { return fmt.Sprint(v...) })
}
func (l *Logger) Warnf(format string, v ...any) {
	l.output(LevelWarn, func() string { return fmt.Sprintf(format, v...) })
}
func (l *Logger) Warn(v ...any) {
	l.output(LevelWarn, func() string { return fmt.Sprint(v...) })
}
func (l *Logger) Errorf(format string, v ...any) {
	l.output(LevelError, func() string { return fmt.Sprintf(format, v...) })
}
func (l *Logger) Error(v ...any) {
	l.output(LevelError, func() string { return fmt.Sprint(v...) })
}
func (l *Logger) Fatalf(format string, v ...any) {
	l.output(LevelFatal, func() string { return fmt.Sprintf(format, v...) })
	os.Exit(1)
}
func (l *Logger) Fatal(v ...any) {
	l.output(LevelFatal, func() string { return fmt.Sprint(v...) })
	os.Exit(1)
}

func (l *Logger) Println(v ...any) {
	l.output(l.levelPriority, func() string { return fmt.Sprint(v...) })
}

func (l *Logger) Printf(format string, v ...any) {
	l.output(l.levelPriority, func() string { return fmt.Sprintf(format, v...) })
}
func (l *Logger) Writer() io.Writer { return l.out }
func (l *Logger) SetlevelPriority(level uint8) *Logger {
	l.levelPriority = level
	return l
}

func New() *Logger {
	return &Logger{
		out:           os.Stderr,
		levelNormal:   LevelInfo,
		callDepth:     3,
		pc:            0,
		levelPriority: 0,
	}
}

var std = New().SetCalldepth(4)

func SetOutput(out io.Writer)        { std.SetOutput(out) }
func SetCaller(on bool)              { std.SetCaller(on) }
func SetlevelNormal(level uint8)     { std.levelNormal = level }
func SetPC(pc uintptr)               { std.SetPC(pc) }
func SetFormat(format uint8)         { std.SetFormat(format) }
func Debugf(format string, v ...any) { std.Debugf(format, v...) }
func Debug(v ...any)                 { std.Debug(v...) }
func Infof(format string, v ...any)  { std.Infof(format, v...) }
func Info(v ...any)                  { std.Info(v...) }
func Warnf(format string, v ...any)  { std.Warnf(format, v...) }
func Warn(v ...any)                  { std.Warn(v...) }
func Errorf(format string, v ...any) { std.Errorf(format, v...) }
func Error(v ...any)                 { std.Error(v...) }
func Fatalf(format string, v ...any) { std.Fatalf(format, v...) }
func Fatal(v ...any)                 { std.Fatal(v...) }
func Println(v ...any)               { std.Println(v...) }
func Printf(format string, v ...any) { std.Printf(format, v...) }
func Writer() io.Writer              { return std.out }

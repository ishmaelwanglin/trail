package trail

import (
	"os"
	"testing"
)

func TestFunc(t *testing.T) {
	file, err := os.OpenFile("mylog.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	l := New().SetOutput(file)
	l.SetTraceEnabled()
	l.SetFormat(0)
	// l.UseChan()
	// defer l.CloseChan()
	// l.Errorf("%s", "hala, madrid")
	l.Infof("%s", "hala, madrid")
	// l.Debugf("%s", "hala, madrid")
	// l.Warnf("%s", "hala, madrid")
	// l.Fatalf("%s", "hala, madrid")
	SetOutput(os.Stderr)
	SetTraceEnabled()
	Infof("%s", "hala, madrid") // 会panic
	// <-time.After(1 * time.Second)
}

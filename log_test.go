package infratrail

import (
	"os"
	"testing"
)

func TestMain(t *testing.T) {
	file, err := os.OpenFile("mylog.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	l := New(file)
	l.SetTraceEnabled()
	// l.UseChan()
	// defer l.CloseChan()
	l.Errorf("%s", "hala, madrid")
	l.Infof("%s", "hala, madrid")
	l.Debugf("%s", "hala, madrid")
	l.Warnf("%s", "hala, madrid")
	// l.Fatalf("%s", "hala, madrid")
	Infof("%s", "hala, madrid")
	// <-time.After(1 * time.Second)
}

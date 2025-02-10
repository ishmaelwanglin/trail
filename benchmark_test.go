package trail

import (
	"os"
	"testing"
)

func BenchmarkLog(b *testing.B) {
	file, err := os.OpenFile("mylog.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, os.ModePerm)
	if err != nil {
		panic(err)
	}
	defer file.Close()
	SetOutput(file)
	SetCaller(true)
	SetPC(10)
	SetFormat(FormatText)
	// l.SetPC(10)
	// // l.UseChan()
	// // defer l.CloseChan()
	// l.Errorf("%s", "hala, madrid")
	// l.Infof("%s", "hala, madrid")
	// l.Debugf("%s", "hala, madrid")
	// l.Warnf("%s", "hala, madrid")
	// l.Fatalf("%s", "hala, madrid")
	Infof("%s", "hala, madrid")
	Errorf("%s", "hala, madrid")

}

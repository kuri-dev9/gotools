package cli

import (
	"runtime"

	"github.com/sirupsen/logrus"
)

var Log = logrus.New()

func init() {
	n := runtime.NumCPU()

	if n <= 0 {
		n = 1
	}

	if n > 64 {
		n = 64
	}

	runtime.GOMAXPROCS(n)
}

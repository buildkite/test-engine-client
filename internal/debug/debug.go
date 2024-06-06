package debug

import (
	"io"
	"log"
	"os"
)

var enabled = false
var logger = log.New(os.Stdout, "DEBUG: ", log.LstdFlags|log.Lmsgprefix)

// Printf works like log.Printf, but only when debugging is enabled.
func Printf(format string, v ...interface{}) {
	if enabled {
		logger.Printf(format, v...)
	}
}

// Println works like log.Println, but only when debugging is enabled.
func Println(v ...interface{}) {
	if enabled {
		logger.Println(v...)
	}
}

func SetDebug(b bool) {
	enabled = b
}

func SetOutput(w io.Writer) {
	logger.SetOutput(w)
}

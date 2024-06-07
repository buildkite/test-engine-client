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

// SetDebug sets the debugging state.
// When debugging is enabled (true), debug.Printf and debug.Println will print to output.
// Output by default is os.Stdout, and can be changed with debug.SetOutput.
func SetDebug(b bool) {
	enabled = b
}

// SetOutput sets the destination for the logger.
// By default, it is set to os.Stdout.
// It can be set to any io.Writer, such as a file or a buffer.
func SetOutput(w io.Writer) {
	logger.SetOutput(w)
}

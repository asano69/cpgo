package main

import (
	"os"

	"github.com/sirupsen/logrus"
)

// logger is the package-wide logger used for all diagnostic output (as
// opposed to the live progress line and the final summary, which are plain
// stdout writes). logrus's default TextFormatter already colors Error-level
// output red and Warn-level output yellow when stderr is a terminal, so
// picking the right level for a message is enough to make important
// problems - like confirmed file corruption - stand out.
var logger = newLogger()

func newLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(os.Stderr)
	l.SetFormatter(&logrus.TextFormatter{
		DisableTimestamp: true,
	})
	return l
}

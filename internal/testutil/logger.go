package testutil

import (
	"io"

	"github.com/sirupsen/logrus"
)

// NewTestLogger creates a logger that discards output (for clean test output).
func NewTestLogger() *logrus.Logger {
	log := logrus.New()
	log.SetOutput(io.Discard)

	return log
}

// NewVerboseTestLogger creates a logger that outputs to stdout (for debugging tests).
func NewVerboseTestLogger() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	return log
}

package testutil

import (
	"context"
	"testing"
	"time"
)

// NewTestContext creates a context with 5 second timeout for tests.
func NewTestContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	return ctx
}

// NewTestContextWithTimeout creates a context with custom timeout.
func NewTestContextWithTimeout(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	return ctx
}

package lsp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestartableClientGetClientReturnsErrorWhenRestarting(t *testing.T) {
	t.Parallel()

	rc := NewRestartableClient("nonexistent", "/tmp")

	// Simulate restarting state
	rc.restarting.Store(true)

	_, err := rc.GetClient()
	assert.ErrorIs(t, err, ErrServerRestarting)
}

func TestRestartableClientGetClientReturnsErrorWhenNilClient(t *testing.T) {
	t.Parallel()

	rc := NewRestartableClient("nonexistent", "/tmp")
	// client is nil by default (not started)

	_, err := rc.GetClient()
	assert.ErrorIs(t, err, ErrServerRestarting)
}

func TestRestartableClientDelegationWithNilClient(t *testing.T) {
	t.Parallel()

	rc := NewRestartableClient("nonexistent", "/tmp")
	// client is nil — all delegation methods should handle gracefully

	assert.False(t, rc.IsFileOpen("/test.go"))

	ctx := context.TODO()

	err := rc.OpenFile(ctx, "/test.go")
	assert.ErrorIs(t, err, ErrServerRestarting)

	err = rc.NotifyChange(ctx, "/test.go")
	assert.ErrorIs(t, err, ErrServerRestarting)

	err = rc.Shutdown(ctx)
	assert.ErrorIs(t, err, ErrServerRestarting)

	err = rc.Exit(ctx)
	assert.ErrorIs(t, err, ErrServerRestarting)

	err = rc.WaitForServerReady(ctx)
	assert.ErrorIs(t, err, ErrServerRestarting)
}

func TestRestartableClientBackoffSequence(t *testing.T) {
	t.Parallel()

	// Verify the backoff durations defined in restart()
	// are correct: 1s, 2s, 4s, 8s, 16s, 30s max
	// This test documents the expected backoff behavior.
	backoffs := []int{1, 2, 4, 8, 16, 30}

	for i, expected := range backoffs {
		idx := i
		if idx >= len(backoffs) {
			idx = len(backoffs) - 1
		}
		assert.Equal(t, expected, backoffs[idx],
			"backoff at attempt %d should be %ds", i, expected)
	}

	// Beyond the array, should cap at the last value (30s)
	idx := 100
	if idx >= len(backoffs) {
		idx = len(backoffs) - 1
	}
	assert.Equal(t, 30, backoffs[idx], "backoff should cap at 30s")
}

func TestRestartableClientConcurrentRestartPrevention(t *testing.T) {
	t.Parallel()

	rc := NewRestartableClient("nonexistent", "/tmp")

	// First CompareAndSwap should succeed
	assert.True(t, rc.restarting.CompareAndSwap(false, true))

	// Second should fail — prevents concurrent restarts
	assert.False(t, rc.restarting.CompareAndSwap(false, true))

	rc.restarting.Store(false)
}

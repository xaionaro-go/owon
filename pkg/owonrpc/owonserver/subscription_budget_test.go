package owonserver

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
)

// TestSubscriptionBudgetAtomicBounds rejects negative charges and integer overflow without changing balance.
//
// Example: reserving MaxInt leaves no space for even one additional byte.
func TestSubscriptionBudgetAtomicBounds(t *testing.T) {
	budget := newSubscriptionBudget(math.MaxInt)
	require.False(t, budget.reserve(-1))
	require.Zero(t, budget.used.Load())
	require.True(t, budget.reserve(math.MaxInt))
	require.False(t, budget.reserve(1))
	require.False(t, budget.reserve(math.MaxInt))
	require.Equal(t, int64(math.MaxInt), budget.used.Load())
	budget.release(math.MaxInt)
	require.Zero(t, budget.used.Load())
	require.Panics(t,
		// releaseUnownedCharge verifies underflow cannot create credit.
		//
		// Example: releasing one byte from zero panics without mutating the account.
		func() { budget.release(1) })
	require.Zero(t, budget.used.Load())
}

// TestSubscriptionBudgetConcurrentReservations admits exactly the shared limit under contention.
//
// Example: 128 simultaneous one-byte reservations cannot exceed a 32-byte allowance.
func TestSubscriptionBudgetConcurrentReservations(t *testing.T) {
	budget := newSubscriptionBudget(32)
	start := make(chan struct{})
	results := make(chan bool, 128)
	for range 128 {
		observability.Go(t.Context(),
			// reserveTogether starts all contenders from the same observable barrier.
			//
			// Example: successful reservations remain owned until the test releases them.
			func(context.Context) { <-start; results <- budget.reserve(1) })
	}
	close(start)
	accepted := 0
	for range 128 {
		if <-results {
			accepted++
		}
	}
	require.Equal(t, 32, accepted)
	require.Equal(t, int64(32), budget.used.Load())
	done := make(chan struct{}, accepted)
	for range accepted {
		observability.Go(t.Context(),
			// releaseTogether returns each distinct charge once under contention.
			//
			// Example: a completed drain leaves the balance exactly zero.
			func(context.Context) { budget.release(1); done <- struct{}{} })
	}
	for range accepted {
		<-done
	}
	require.Zero(t, budget.used.Load())
}

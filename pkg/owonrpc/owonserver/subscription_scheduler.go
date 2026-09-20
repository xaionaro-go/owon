package owonserver

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonrpc"
)

const (
	// defaultMaximumSubscriptionPollWork is the weighted command budget per second.
	//
	// Example: the default server admits at most 64 estimated transport commands per window.
	defaultMaximumSubscriptionPollWork = 64
	// maximumSubscriptionPollWorkCapacity bounds configured weighted command work.
	//
	// Example: a startup typo cannot allocate an unbounded scheduler budget.
	maximumSubscriptionPollWorkCapacity = 1 << 20
	// subscriptionPollWorkWindow is the duration over which the server-wide budget is spread.
	//
	// Example: 64 command units are spaced across one second by default.
	subscriptionPollWorkWindow = time.Second
	// maximumSubscriptionPollWeight is the largest validated subscription poll.
	//
	// Example: all measurements, one state header, and two waveform captures fit in one window.
	maximumSubscriptionPollWeight = 1 + owonmodel.MaximumMeasurementSelectors + 2 + 2*owonrpc.MaximumSubscriptionWaveformChannels
)

// subscriptionPollScheduler admits weighted polling work in FIFO order at a fixed rate.
//
// Example: a broad waveform poll cannot be bypassed by later light polls, and canceled
// waiters are removed before the next budget window.
type subscriptionPollScheduler struct {
	mu               sync.Mutex
	capacity         int
	workUnitInterval time.Duration
	committedUntil   time.Time
	queue            []*subscriptionPollRequest
}

// subscriptionPollRequest represents one queued weighted poll.
//
// Example: a request charging two command units waits for both units to be available.
type subscriptionPollRequest struct {
	weight  int
	readyAt time.Time
	endAt   time.Time
	granted chan struct{}
}

// newSubscriptionPollScheduler constructs a weighted rate scheduler.
//
// Example: production uses a 64-command-per-second budget while unit tests use larger spacings.
func newSubscriptionPollScheduler(
	capacity int,
	window time.Duration,
) (*subscriptionPollScheduler, error) {
	if capacity <= 0 {
		return nil, fmt.Errorf("subscription poll budget %d must be positive: %w", capacity, &ErrInvalidConfig{Reason: "poll budget must be positive"})
	}
	if window <= 0 {
		return nil, fmt.Errorf("subscription poll budget window %s must be positive: %w", window, &ErrInvalidConfig{Reason: "poll budget window must be positive"})
	}
	workUnitInterval := window / time.Duration(capacity)
	if workUnitInterval <= 0 {
		return nil, fmt.Errorf("subscription poll budget %d is too large for window %s: %w", capacity, window, &ErrInvalidConfig{Reason: "poll budget has sub-nanosecond work units"})
	}

	now := time.Now()
	return &subscriptionPollScheduler{
		capacity:         capacity,
		workUnitInterval: workUnitInterval,
		committedUntil:   now,
	}, nil
}

// acquire waits until one weighted poll is admitted or its context is canceled.
//
// Example: a stream canceled while waiting does not consume a future rate slot.
func (scheduler *subscriptionPollScheduler) acquire(
	ctx context.Context,
	weight int,
) error {
	if scheduler == nil {
		return &ErrUnavailable{Operation: "wait for subscription poll work", Resource: "scheduler", Reason: "is unavailable"}
	}
	if ctx == nil {
		return &ErrUnavailable{Operation: "wait for subscription poll work", Resource: "context", Reason: "is nil"}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for subscription poll work: %w", err)
	}
	request, err := scheduler.enqueue(weight)
	if err != nil {
		return err
	}

	return scheduler.wait(ctx, request)
}

// wait observes one queued request until admission or cancellation.
//
// Example: a test can stage a queued request and cancel it without using wall-clock sleeps.
func (scheduler *subscriptionPollScheduler) wait(
	ctx context.Context,
	request *subscriptionPollRequest,
) error {
	if scheduler == nil {
		return &ErrUnavailable{Operation: "wait for subscription poll work", Resource: "scheduler", Reason: "is unavailable"}
	}
	if ctx == nil {
		return &ErrUnavailable{Operation: "wait for subscription poll work", Resource: "context", Reason: "is nil"}
	}
	if request == nil {
		return &ErrInvalidInput{Reason: "wait for subscription poll work: request is nil"}
	}

	for {
		wait := scheduler.dispatchAndNextWakeup(time.Now())

		timer := time.NewTimer(wait)
		select {
		case <-request.granted:
			stopTimer(timer)
			return nil
		case <-ctx.Done():
			stopTimer(timer)
			scheduler.cancel(request)
			return fmt.Errorf("wait for subscription poll work: %w", ctx.Err())
		case <-timer.C:
			scheduler.dispatch(time.Now())
		}
	}
}

// dispatchAndNextWakeup atomically dispatches ready work and computes the next bounded wakeup.
//
// Example: wait never holds scheduler.mu while a timer or grant channel blocks.
func (scheduler *subscriptionPollScheduler) dispatchAndNextWakeup(now time.Time) time.Duration {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.dispatchLocked(now)

	return scheduler.nextWakeupLocked(now)
}

// stopTimer releases a timer without blocking when its notification raced with cancellation.
//
// Example: a grant and context cancellation arriving together cannot strand the waiter.
func stopTimer(timer *time.Timer) {
	if timer == nil || timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}

// enqueue adds one request and immediately grants work whose rate slot is available.
//
// Example: the first request does not wait for a timer while later work observes its spacing.
func (scheduler *subscriptionPollScheduler) enqueue(weight int) (*subscriptionPollRequest, error) {
	if scheduler == nil {
		return nil, &ErrUnavailable{Operation: "enqueue subscription poll work", Resource: "scheduler", Reason: "is unavailable"}
	}
	if weight <= 0 {
		return nil, fmt.Errorf("subscription poll weight %d must be positive: %w", weight, &ErrInvalidInput{Reason: "poll weight must be positive"})
	}

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if weight > scheduler.capacity {
		return nil, fmt.Errorf("subscription poll weight %d exceeds budget %d: %w", weight, scheduler.capacity, new(ErrSubscriptionResourceExhausted))
	}
	request := &subscriptionPollRequest{weight: weight, granted: make(chan struct{})}
	now := time.Now()
	scheduler.queue = append(scheduler.queue, request)
	scheduler.dispatchLocked(now)

	return request, nil
}

// cancel removes a request that has not yet been granted and wakes the next waiter.
//
// Example: cancellation of a queued waveform poll releases its place for a lighter poll.
func (scheduler *subscriptionPollScheduler) cancel(request *subscriptionPollRequest) {
	if scheduler == nil || request == nil {
		return
	}

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	for index, queued := range scheduler.queue {
		if queued != request {
			continue
		}
		copy(scheduler.queue[index:], scheduler.queue[index+1:])
		scheduler.queue[len(scheduler.queue)-1] = nil
		scheduler.queue = scheduler.queue[:len(scheduler.queue)-1]
		scheduler.dispatchLocked(time.Now())
		return
	}
}

// dispatch grants queued work whose weighted rate slot has arrived.
//
// Example: a test or a timer advances the rate clock without bypassing the oldest request.
func (scheduler *subscriptionPollScheduler) dispatch(now time.Time) {
	if scheduler == nil {
		return
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	scheduler.dispatchLocked(now)
}

// dispatchLocked grants only the FIFO prefix whose scheduled rate slots have arrived.
//
// Example: a later one-unit request cannot jump ahead of an older two-unit request.
func (scheduler *subscriptionPollScheduler) dispatchLocked(now time.Time) {
	for len(scheduler.queue) != 0 {
		scheduler.scheduleQueuedLocked(now)
		request := scheduler.queue[0]
		if request.readyAt.After(now) {
			return
		}
		if request.readyAt.Before(now) {
			request.readyAt = now
			request.endAt = now.Add(scheduler.workDuration(request.weight))
		}
		scheduler.queue[0] = nil
		scheduler.queue = scheduler.queue[1:]
		scheduler.committedUntil = request.endAt
		close(request.granted)
	}
}

// scheduleQueuedLocked assigns weighted rate slots after committed work.
//
// Example: a two-unit poll reserves twice the one-unit spacing before the next poll.
func (scheduler *subscriptionPollScheduler) scheduleQueuedLocked(now time.Time) {
	base := now
	if scheduler.committedUntil.After(base) {
		base = scheduler.committedUntil
	}
	for _, request := range scheduler.queue {
		request.readyAt = base
		request.endAt = base.Add(scheduler.workDuration(request.weight))
		base = request.endAt
	}
}

// workDuration converts command units into the scheduler's elapsed rate cost.
//
// Example: a four-command poll at 64 commands per second reserves 62.5 milliseconds.
func (scheduler *subscriptionPollScheduler) workDuration(weight int) time.Duration {
	return time.Duration(weight) * scheduler.workUnitInterval
}

// nextWakeupLocked returns a positive timer duration for a queued request.
//
// Example: each blocked waiter wakes when the oldest weighted rate slot arrives.
func (scheduler *subscriptionPollScheduler) nextWakeupLocked(now time.Time) time.Duration {
	if len(scheduler.queue) == 0 {
		return time.Nanosecond
	}
	wait := scheduler.queue[0].readyAt.Sub(now)
	if wait <= 0 {
		return time.Nanosecond
	}

	return wait
}

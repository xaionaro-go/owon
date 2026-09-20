package owonsession

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/owon/pkg/owonmodel"
	"github.com/xaionaro-go/owon/pkg/owonprotocol"
)

// ErrUnavailable identifies a session resource that cannot perform an operation.
//
// Example: executing a command after closing the session returns this error.
type ErrUnavailable struct {
	Operation string
	Resource  string
	Reason    string
}

// Error identifies the operation and unavailable resource.
//
// Example: a closed session names the rejected execution operation.
func (err *ErrUnavailable) Error() string {
	if err == nil {
		return "OWON session unavailable"
	}
	return fmt.Sprintf("%s: %s %s", err.Operation, err.Resource, err.Reason)
}

// Unwrap returns nil because resource state is a leaf error.
//
// Example: errors.As matches the unavailable session through an instrument error.
func (*ErrUnavailable) Unwrap() error {
	return nil
}

// Backend owns one concrete device session and performs one already-validated exchange.
//
// Example: owonusb.Backend implements this interface for bulk endpoint communication.
type Backend interface {
	// Exchange executes exactly one command without retrying it.
	//
	// Example: a timeout may mean the device accepted the command, so callers treat it as ambiguous.
	Exchange(
		context.Context,
		owonprotocol.Command,
	) ([]byte, error)
	// ReopenAndValidate replaces the session and proves the expected serial and SCPI identity.
	//
	// Example: a poisoned session calls this before admitting its next command.
	ReopenAndValidate(
		context.Context,
		owonmodel.SerialNumber,
	) error
	// Close releases the current device session, observing cancellation where the backend permits.
	// Native cleanup may outlive the context; Session.CloseContext bounds only its caller's wait.
	//
	// Example: daemon shutdown closes the libusb context and claimed interface.
	Close(context.Context) error
}

// Session serializes access to the device and quarantines sessions after ambiguous failures.
// A Session must not be copied after construction.
//
// Example: concurrent RPC handlers can share one Session without interleaving USB transactions.
type Session struct {
	backend Backend
	config  Config

	admission chan struct{}
	poisoned  bool
	closed    atomic.Bool

	closeMu      sync.Mutex
	closeAttempt *sessionCloseAttempt
}

// sessionCloseAttempt owns one immutable close-generation result and signal.
//
// Example: a failed generation retains its own error while a retry starts another attempt.
type sessionCloseAttempt struct {
	done chan struct{}
	err  error
}

// sessionCloseRunner owns the observability callback for one close generation.
//
// Example: its Run method closes the generation signal after backend cleanup finishes.
type sessionCloseRunner struct {
	Session *Session
	Attempt *sessionCloseAttempt
}

// Transaction owns one session admission across a complete logical operation.
// Concurrent Execute calls are serialized; Close waits for admitted native work.
// A Transaction must not be copied after construction.
//
// Example: waveform header and channel reads cannot interleave with another caller.
type Transaction struct {
	session        *Session
	execution      chan struct{}
	closeRequested chan struct{}
	closeDone      chan struct{}
	closed         atomic.Bool
	failed         error // Guarded by execution, including failure publication before closure.
}

// New constructs a serialized session around one validated backend.
//
// Example: New(backend, Config{ExpectedSerial: "25061855"}) binds recovery to that instrument.
func New(
	backend Backend,
	config Config,
) (*Session, error) {
	expectedSerial := owonmodel.SerialNumber(strings.TrimSpace(string(config.ExpectedSerial)))
	if backend == nil || isNilBackend(backend) {
		return nil, &ErrUnavailable{Operation: "create OWON session", Resource: "backend", Reason: "is nil"}
	}
	if expectedSerial == "" {
		return nil, &ErrInvalidConfig{Reason: "create OWON session: empty expected serial"}
	}
	timeout, err := NormalizeOperationTimeout(config.OperationTimeout)
	if err != nil {
		return nil, fmt.Errorf("create OWON session: %w", err)
	}

	return &Session{
		backend:   backend,
		config:    Config{ExpectedSerial: expectedSerial, OperationTimeout: timeout},
		admission: make(chan struct{}, 1),
	}, nil
}

// isNilBackend detects an interface containing a typed nil backend pointer.
//
// Example: New rejects a nil *owonusb.Backend before any method dispatch.
func isNilBackend(backend Backend) bool {
	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// Execute admits, serializes, and performs one command without automatic replay.
//
// Example: an ambiguous write error poisons the session and is returned to the caller.
func (session *Session) Execute(
	ctx context.Context,
	command owonprotocol.Command,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Session.Execute")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Session.Execute: %v", _err) }()
	}

	if session == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "session", Reason: "is nil"}
	}
	if ctx == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "context", Reason: "is nil"}
	}
	if session.closed.Load() {
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "session", Reason: "is closed"}
	}
	if session.backend == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "backend", Reason: "is unavailable"}
	}
	if session.admission == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "admission", Reason: "is unavailable"}
	}
	if err := owonprotocol.ValidateCommand(command); err != nil {
		return nil, fmt.Errorf("execute OWON command: %w", err)
	}
	transaction, err := session.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer transaction.Close()

	return transaction.Execute(ctx, command)
}

// Begin acquires cancellable admission and recovers a poisoned session once.
//
// Example: callers close the returned transaction after every command in their operation.
func (session *Session) Begin(ctx context.Context) (_result *Transaction, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Session.Begin")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Session.Begin: %v", _err) }()
	}

	if session == nil {
		return nil, &ErrUnavailable{Operation: "begin OWON transaction", Resource: "session", Reason: "is nil"}
	}
	if ctx == nil {
		return nil, &ErrUnavailable{Operation: "begin OWON transaction", Resource: "context", Reason: "is nil"}
	}
	if session.closed.Load() {
		return nil, &ErrUnavailable{Operation: "begin OWON transaction", Resource: "session", Reason: "is closed"}
	}
	if session.backend == nil {
		return nil, &ErrUnavailable{Operation: "begin OWON transaction", Resource: "backend", Reason: "is unavailable"}
	}
	if session.admission == nil {
		return nil, &ErrUnavailable{Operation: "begin OWON transaction", Resource: "admission", Reason: "is unavailable"}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("begin OWON transaction before admission: %w", err)
	}

	select {
	case session.admission <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for OWON session admission: %w", ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		session.releaseAdmission()
		return nil, fmt.Errorf("begin OWON transaction after admission: %w", err)
	}
	if session.closed.Load() {
		session.releaseAdmission()
		return nil, &ErrUnavailable{Operation: "execute OWON command", Resource: "session", Reason: "is closed"}
	}
	if session.poisoned {
		logger.Debugf(ctx, "recovering quarantined session for serial %q", session.config.ExpectedSerial)
		if err := session.recover(ctx); err != nil {
			session.releaseAdmission()
			return nil, fmt.Errorf("recover poisoned session for serial %q: %w", session.config.ExpectedSerial, err)
		}
		session.poisoned = false
		logger.Debugf(ctx, "session recovery validated for serial %q", session.config.ExpectedSerial)
	}
	logger.Debugf(ctx, "transaction admitted for serial %q", session.config.ExpectedSerial)

	return &Transaction{
		session:        session,
		execution:      make(chan struct{}, 1),
		closeRequested: make(chan struct{}),
		closeDone:      make(chan struct{}),
	}, nil
}

// recover sets one cooperative recovery deadline and retains admission until the backend returns.
//
// Example: a stuck native callback cannot release ownership and overlap a second session.
func (session *Session) recover(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Session.recover")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Session.recover: %v", _err) }()
	}

	ctx, cancel := context.WithTimeout(ctx, session.config.OperationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	return session.backend.ReopenAndValidate(ctx, session.config.ExpectedSerial)
}

// Execute serializes one command within the transaction without reacquiring session admission.
// Waiting observes caller cancellation and transaction closure without spending the device budget.
//
// Example: after one ambiguous failure, later commands in the same operation are skipped.
func (transaction *Transaction) Execute(
	ctx context.Context,
	command owonprotocol.Command,
) (_result []byte, _err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Transaction.Execute")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Transaction.Execute: %v", _err) }()
	}

	if transaction == nil || transaction.session == nil || transaction.execution == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "transaction", Reason: "is unavailable"}
	}
	if ctx == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "context", Reason: "is nil"}
	}
	if transaction.closed.Load() {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "transaction", Reason: "is closed"}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("execute OWON transaction before execution admission: %w", err)
	}

	select {
	case transaction.execution <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for OWON transaction execution: %w", ctx.Err())
	case <-transaction.closeRequested:
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "transaction", Reason: "is closed"}
	}
	defer transaction.releaseExecution()

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("execute OWON transaction after execution admission: %w", err)
	}
	// This recheck linearizes admission against Close. An already-admitted
	// exchange retains both gates until native completion and failure publication.
	if transaction.closed.Load() {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "transaction", Reason: "is closed"}
	}
	if transaction.failed != nil {
		return nil, fmt.Errorf("execute OWON transaction after failure: %w", transaction.failed)
	}
	if err := owonprotocol.ValidateCommand(command); err != nil {
		return nil, fmt.Errorf("execute OWON transaction: %w", err)
	}
	if transaction.session.closed.Load() {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "session", Reason: "is closed"}
	}
	if transaction.session.backend == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "backend", Reason: "is unavailable"}
	}
	if transaction.session.admission == nil {
		return nil, &ErrUnavailable{Operation: "execute OWON transaction", Resource: "admission", Reason: "is unavailable"}
	}
	// Bound the complete exchange, not each fragment or time spent waiting for admission.
	ctx, cancel := context.WithTimeout(ctx, transaction.session.config.OperationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("execute OWON transaction before I/O: %w", err)
	}
	response, err := transaction.session.backend.Exchange(ctx, command)
	if err != nil {
		transaction.session.poisoned = true
		transaction.failed = fmt.Errorf("exchange OWON command %q: %w", command.Text, err)
		logger.Warnf(ctx, "session quarantined after ambiguous exchange: %v", err)
		return nil, transaction.failed
	}

	return response, nil
}

// OperationContext derives the configured post-admission operation budget from
// a live transaction and caller context.
//
// Example: a DMM convergence operation keeps caller admission cancellation but
// receives the session's configured budget after Begin has succeeded.
func (transaction *Transaction) OperationContext(
	parent context.Context,
) (context.Context, context.CancelFunc, error) {
	if transaction == nil || transaction.session == nil || transaction.execution == nil {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "transaction", Reason: "is unavailable"}
	}
	if parent == nil {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "context", Reason: "is nil"}
	}
	if transaction.closed.Load() {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "transaction", Reason: "is closed"}
	}
	session := transaction.session
	if session.closed.Load() {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "session", Reason: "is closed"}
	}
	if session.backend == nil {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "backend", Reason: "is unavailable"}
	}
	if session.admission == nil {
		return nil, nil, &ErrUnavailable{Operation: "create OWON operation context", Resource: "admission", Reason: "is unavailable"}
	}

	operationContext, cancel := context.WithTimeout(parent, session.config.OperationTimeout)
	return operationContext, cancel, nil
}

// Close prevents further execution and waits for admitted work before releasing session admission.
// Concurrent calls join the same closure. Close may block indefinitely for an uncooperative
// backend: caller cancellation or a device deadline cannot detach native ownership.
//
// Example: callers defer Close immediately after a successful Begin.
func (transaction *Transaction) Close() {
	if transaction == nil || transaction.session == nil || transaction.execution == nil {
		return
	}
	if transaction.closed.Swap(true) {
		<-transaction.closeDone
		return
	}

	close(transaction.closeRequested)
	transaction.execution <- struct{}{}
	defer transaction.releaseExecution()
	transaction.session.releaseAdmission()
	close(transaction.closeDone)
}

// releaseExecution allows the next command or final closure to observe completed native work.
//
// Example: Execute defers this until its failure has been recorded under the execution gate.
func (transaction *Transaction) releaseExecution() {
	<-transaction.execution
}

// releaseAdmission makes the serialized device available to the next caller.
//
// Example: Execute defers it so every success and error path releases exactly once.
func (session *Session) releaseAdmission() {
	if session == nil || session.admission == nil {
		return
	}
	<-session.admission
}

// Close prevents future exchanges and releases the backend once current work finishes.
//
// Example: simple callers use Close when they have already drained all requests.
func (session *Session) Close() error {
	return session.CloseContext(context.Background())
}

// CloseContext prevents future exchanges and bounds only the caller's cleanup wait.
//
// Example: its deadline can return before non-interruptible native libusb cleanup completes.
func (session *Session) CloseContext(ctx context.Context) (_err error) {
	if ctx != nil {
		logger.Tracef(ctx, "Session.CloseContext")
		defer
		// traceResult records completion without logging response payloads.
		//
		// Example: a failed operation retains its contextual diagnostic fields.
		func() { logger.Tracef(ctx, "/Session.CloseContext: %v", _err) }()
	}

	if session == nil {
		return nil
	}
	if ctx == nil {
		return &ErrUnavailable{Operation: "close OWON session", Resource: "context", Reason: "is nil"}
	}
	if session.backend == nil {
		return &ErrUnavailable{Operation: "close OWON session", Resource: "backend", Reason: "is unavailable"}
	}
	if session.admission == nil {
		return &ErrUnavailable{Operation: "close OWON session", Resource: "admission", Reason: "is unavailable"}
	}

	session.closed.Store(true)
	attempt := session.getOrStartCloseAttempt(ctx)
	select {
	case <-attempt.done:
		return attempt.err
	case <-ctx.Done():
		return fmt.Errorf("wait to close OWON session: %w", ctx.Err())
	}
}

// getOrStartCloseAttempt returns the active close generation or creates and launches one.
//
// Example: concurrent CloseContext callers share one backend cleanup attempt.
func (session *Session) getOrStartCloseAttempt(ctx context.Context) *sessionCloseAttempt {
	session.closeMu.Lock()
	defer session.closeMu.Unlock()
	if session.closeAttempt != nil {
		return session.closeAttempt
	}
	attempt := &sessionCloseAttempt{done: make(chan struct{})}
	session.closeAttempt = attempt
	observability.Go(context.WithoutCancel(ctx), sessionCloseRunner{Session: session, Attempt: attempt}.Run)

	return attempt
}

// Run performs backend cleanup for one close generation.
//
// Example: observability.Go invokes Run without capturing mutable loop state in a closure.
func (runner sessionCloseRunner) Run(ctx context.Context) {
	runner.Session.closeBackend(ctx, runner.Attempt)
}

// closeBackend waits for the in-flight transaction and releases resources exactly once.
//
// Example: a timed-out CloseContext can return while this cleanup continues safely.
func (session *Session) closeBackend(
	ctx context.Context,
	attempt *sessionCloseAttempt,
) {
	logger.Tracef(ctx, "Session.closeBackend")
	session.admission <- struct{}{}
	logger.Debugf(ctx, "closing backend after active transaction completed")
	err := session.backend.Close(ctx)
	logger.Tracef(ctx, "/Session.closeBackend: %v", err)
	<-session.admission
	if err != nil {
		err = fmt.Errorf("close OWON backend: %w", err)
	}
	session.closeMu.Lock()
	defer session.closeMu.Unlock()
	attempt.err = err
	if err != nil && session.closeAttempt == attempt {
		session.closeAttempt = nil
	}
	close(attempt.done)
}

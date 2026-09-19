// Package owonlog installs structured go-belt logging at process and transport boundaries.
//
// Example: a command passes its diagnostic writer to WithContext before starting work.
package owonlog

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	beltlogrus "github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/sirupsen/logrus"
)

// Config identifies process logging policy without retaining runtime context.
//
// Example: Config{Level: logger.LevelDebug, Service: "owond"} enables request diagnostics.
type Config struct {
	Level   logger.Level
	Service string
}

// Initializer retains validated configuration for callbacks that cannot return errors.
//
// Example: gRPC connection tagging creates a contextual logger from one immutable initializer.
type Initializer struct {
	config Config
	output io.Writer
}

// NewInitializer validates configuration once before installing infallible transport callbacks.
//
// Example: an invalid logging level fails server startup rather than a connection callback.
func NewInitializer(
	config Config,
	output io.Writer,
) (*Initializer, error) {
	if config.Level < logger.LevelFatal || config.Level > logger.LevelTrace {
		return nil, &ErrInvalidConfig{Reason: fmt.Sprintf("invalid logging level %d", config.Level)}
	}
	if output == nil {
		return nil, &ErrInvalidConfig{Reason: "logging output is nil"}
	}
	value := reflect.ValueOf(output)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, &ErrInvalidConfig{Reason: "logging output is typed nil"}
		}
	}
	return &Initializer{config: config, output: output}, nil
}

// WithContext installs a fresh backend while retaining existing context values and fields.
// It requires the nonnil context supplied by the transport callback contract.
//
// Example: TagConn uses this method after startup validated the configuration.
func (initializer *Initializer) WithContext(ctx context.Context) context.Context {
	backend := logrus.New()
	backend.SetOutput(initializer.output)
	backend.SetFormatter(&logrus.TextFormatter{DisableColors: true})
	backend.SetLevel(beltlogrus.LevelToLogrus(initializer.config.Level))
	ctx = logger.CtxWithLogger(ctx, beltlogrus.New(backend).WithLevel(initializer.config.Level))
	return belt.WithField(ctx, "service", initializer.config.Service)
}

// WithContext validates initialization and installs a structured logger in a caller context.
//
// Example: main selects stderr and libraries receive only the resulting context.
func WithContext(
	ctx context.Context,
	config Config,
	output io.Writer,
) (context.Context, error) {
	if ctx == nil {
		return nil, &ErrInvalidConfig{Reason: "logging context is nil"}
	}
	initializer, err := NewInitializer(config, output)
	if err != nil {
		return nil, err
	}
	return initializer.WithContext(ctx), nil
}

// ErrInvalidConfig identifies logging configuration that cannot initialize a backend.
//
// Example: nil output is rejected before request processing starts.
type ErrInvalidConfig struct {
	Reason string
}

// Error describes the rejected logging configuration.
//
// Example: a missing writer reports that logging output is nil.
func (err *ErrInvalidConfig) Error() string {
	if err == nil {
		return "invalid logging configuration"
	}
	return "invalid logging configuration: " + err.Reason
}

// Unwrap reports that local validation failures have no lower-level cause.
//
// Example: errors.As still identifies ErrInvalidConfig without a synthetic cause.
func (err *ErrInvalidConfig) Unwrap() error { return nil }

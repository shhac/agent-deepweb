// Package errors defines the APIError type used across agent-deepweb.
// Every error surfaced to the user/LLM carries a fixable_by classification
// and an optional human-readable hint so the consumer knows whether to
// retry, fix their input, or escalate to a human.
package errors

import (
	"errors"
	"fmt"
)

type FixableBy string

const (
	FixableByAgent FixableBy = "agent"
	FixableByHuman FixableBy = "human"
	FixableByRetry FixableBy = "retry"
)

type APIError struct {
	Message   string    `json:"error"`
	Hint      string    `json:"hint,omitempty"`
	FixableBy FixableBy `json:"fixable_by"`
	Cause     error     `json:"-"`
}

func (e *APIError) Error() string { return e.Message }
func (e *APIError) Unwrap() error { return e.Cause }

func New(message string, fixableBy FixableBy) *APIError {
	return &APIError{Message: message, FixableBy: fixableBy}
}

func Newf(fixableBy FixableBy, format string, args ...any) *APIError {
	return &APIError{Message: fmt.Sprintf(format, args...), FixableBy: fixableBy}
}

func Wrap(err error, fixableBy FixableBy) *APIError {
	if err == nil {
		return nil
	}
	var existing *APIError
	if errors.As(err, &existing) {
		return existing
	}
	return &APIError{Message: err.Error(), FixableBy: fixableBy, Cause: err}
}

func (e *APIError) WithHint(hint string) *APIError {
	e.Hint = hint
	return e
}

func (e *APIError) WithCause(cause error) *APIError {
	e.Cause = cause
	return e
}

func As(err error, target any) bool {
	return errors.As(err, target)
}

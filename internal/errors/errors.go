// Package errors re-exports the shared error contract from lib-agent-output so
// the rest of agent-deepweb keeps the internal/errors import path while the
// implementation lives in one place. Every error surfaced to the user/LLM
// carries a fixable_by classification and an optional human-readable hint so
// the consumer knows whether to retry, fix their input, or escalate to a human.
//
// (Migration shim — call sites can later be pointed at lib-agent-output
// directly and this package deleted.)
package errors

import (
	stderrors "errors"

	out "github.com/shhac/lib-agent-output"
)

type (
	FixableBy = out.FixableBy
	// APIError is agent-deepweb's family name for the shared output.Error type.
	// It carries the same Message/FixableBy/Hint/Cause fields and the same
	// WithHint/WithCause chaining methods the CLI relies on.
	APIError = out.Error
)

const (
	FixableByAgent = out.FixableByAgent
	FixableByHuman = out.FixableByHuman
	FixableByRetry = out.FixableByRetry
)

var (
	New  = out.New
	Newf = out.Newf
)

// Wrap classifies an existing error, preserving an already-classified
// *APIError unchanged (its original fixable_by/hint win). This dedup is
// agent-deepweb policy: the shared out.Wrap always re-wraps with the new
// classification, which would override a previously-set fixable_by when a
// classified error flows back through Wrap. Nil-safe like out.Wrap.
func Wrap(err error, fixableBy FixableBy) *APIError {
	if err == nil {
		return nil
	}
	var existing *APIError
	if stderrors.As(err, &existing) {
		return existing
	}
	return out.Wrap(err, fixableBy)
}

// As keeps the loose target signature the rest of the package expects
// (callers pass **APIError, but the loose any avoids forcing the concrete
// type at every site).
func As(err error, target any) bool {
	return stderrors.As(err, target)
}

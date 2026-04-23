package credential

import (
	"errors"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// WrapNotFound returns a fixable_by:agent APIError with a consistent hint
// if err is a *NotFoundError. Returns nil otherwise, so callers can write:
//
//	if ae := credential.WrapNotFound(err, name); ae != nil { return shared.Fail(ae) }
func WrapNotFound(err error, name string) *agenterrors.APIError {
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		return nil
	}
	return agenterrors.Newf(agenterrors.FixableByAgent,
		"profile %q not found", name).
		WithHint("Run 'agent-deepweb profile list' to see available profiles")
}

// ClassifyLookupErr converts an error from Resolve/GetMetadata into the
// standard APIError: NotFound → fixable_by:agent (with hint), anything
// else → fixable_by:human (wrap). nil error passes through.
// Callers use this as `return shared.Fail(credential.ClassifyLookupErr(err, name))`.
func ClassifyLookupErr(err error, name string) error {
	if err == nil {
		return nil
	}
	if ae := WrapNotFound(err, name); ae != nil {
		return ae
	}
	return agenterrors.Wrap(err, agenterrors.FixableByHuman)
}

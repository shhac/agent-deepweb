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
		"credential %q not found", name).
		WithHint("Run 'agent-deepweb creds list' to see available credentials")
}

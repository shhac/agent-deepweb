package template

import (
	"errors"

	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
)

// WrapNotFound returns a fixable_by:agent APIError if err is *NotFoundError,
// else nil. Use alongside shared.Fail in CLI handlers.
func WrapNotFound(err error, name string) *agenterrors.APIError {
	var nf *NotFoundError
	if !errors.As(err, &nf) {
		return nil
	}
	return agenterrors.Newf(agenterrors.FixableByAgent,
		"template %q not found", name).
		WithHint("Run 'agent-deepweb tpl list' to see available templates")
}

// ClassifyLookupErr mirrors credential.ClassifyLookupErr for templates:
// NotFound → fixable_by:agent, else fixable_by:human.
func ClassifyLookupErr(err error, name string) error {
	if err == nil {
		return nil
	}
	if ae := WrapNotFound(err, name); ae != nil {
		return ae
	}
	return agenterrors.Wrap(err, agenterrors.FixableByHuman)
}

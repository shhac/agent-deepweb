package cli

import (
	"github.com/shhac/agent-deepweb/internal/audit"
	"github.com/shhac/agent-deepweb/internal/config"
	"github.com/shhac/agent-deepweb/internal/credential"
	"github.com/shhac/agent-deepweb/internal/track"
)

// App bundles the injectable process-wide dependencies: the config
// store that backs 'config get/set/unset', the audit writer that
// records every request, the track recorder that persists --track
// records, and the secret backend that stores credentials.
//
// main.go builds exactly one App at startup (via DefaultApp) and
// passes it to Execute. Tests that need hermetic state — a scratch
// config dir with no keychain writes — can construct an App with
// different values and call (*App).Execute directly.
//
// The interface surface here is deliberately thin: App is a
// composition root, not a service locator. Internal packages still
// call their own package-level defaults (config.Read, audit.Append,
// etc.); App is where those defaults are *set*, and the only place
// tests have to look to understand "what does production wire up".
type App struct {
	Config        *config.Store
	Audit         audit.Writer
	Track         track.Recorder
	SecretBackend credential.SecretBackend
}

// DefaultApp returns an App wired to the process-wide defaults:
// the default config store (env-resolved dir), the file-backed audit
// writer, the filesystem track recorder, and the platform-appropriate
// secret backend (keychain on macOS, noop elsewhere).
func DefaultApp() *App {
	return &App{
		Config:        config.NewStore(""),
		Audit:         audit.DefaultWriter,
		Track:         track.DefaultRecorder,
		SecretBackend: credential.DefaultBackend,
	}
}

// install writes the App's dependencies into the package-level
// globals that the rest of the codebase reads from. Called once by
// Execute before the cobra tree runs. Tests that want full isolation
// can skip Execute and drive the cobra commands directly after
// installing a custom App.
func (a *App) install() {
	if a.SecretBackend != nil {
		credential.DefaultBackend = a.SecretBackend
	}
	// config.Store, audit.Writer, and track.Recorder are consumed via
	// package-level defaults today. The App holds them so a future
	// refactor that threads them directly has a single starting point.
}

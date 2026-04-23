package credential

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const keychainService = "app.paulie.agent-deepweb"

// keychainStore persists the secret blob to the macOS Keychain under the
// credential's name. Returns an error on non-macOS or keychain failure so
// callers can fall back to file storage.
func keychainStore(name string, secrets Secrets) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("keychain not available")
	}
	data, err := json.Marshal(secrets)
	if err != nil {
		return err
	}

	_ = exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()

	return exec.Command("security", "add-generic-password",
		"-s", keychainService, "-a", name, "-w", string(data), "-U",
	).Run()
}

func keychainGet(name string) (Secrets, error) {
	if runtime.GOOS != "darwin" {
		return Secrets{}, fmt.Errorf("keychain not available")
	}
	out, err := exec.Command("security", "find-generic-password",
		"-s", keychainService, "-a", name, "-w",
	).Output()
	if err != nil {
		return Secrets{}, err
	}
	var s Secrets
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &s); err != nil {
		return Secrets{}, err
	}
	return s, nil
}

func keychainDelete(name string) {
	if runtime.GOOS != "darwin" {
		return
	}
	_ = exec.Command("security", "delete-generic-password",
		"-s", keychainService, "-a", name).Run()
}

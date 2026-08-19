package services

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"YourQL/pkg/models"
)

// ActiveDatabaseInfo is returned to the frontend to describe which SQLite
// file YourQL's app-state database is currently pointed at.
type ActiveDatabaseInfo struct {
	Path      string `json:"path"`
	IsDefault bool   `json:"is_default"`
}

// GetActiveDatabaseInfo returns the path of the currently active app
// database and whether it is the unmodified default location, for display
// in Settings.
func GetActiveDatabaseInfo() (*ActiveDatabaseInfo, error) {
	// The authoritative answer is resolveDBPath(): it accounts for the
	// pointer file with a silent fallback to the default. Mirror that logic
	// here without exporting resolveDBPath.
	pointerPath := models.ActiveDBPointerPath()
	if pointerPath == "" {
		return &ActiveDatabaseInfo{Path: models.DefaultDBPath(), IsDefault: true}, nil
	}
	data, err := os.ReadFile(pointerPath)
	if err != nil {
		return &ActiveDatabaseInfo{Path: models.DefaultDBPath(), IsDefault: true}, nil
	}
	var ptr models.ActiveDBPointer
	if err := json.Unmarshal(data, &ptr); err != nil || ptr.Path == "" {
		return &ActiveDatabaseInfo{Path: models.DefaultDBPath(), IsDefault: true}, nil
	}
	return &ActiveDatabaseInfo{Path: ptr.Path, IsDefault: false}, nil
}

// SwitchActiveDatabase points YourQL at a different SQLite file on next
// launch and restarts the app to pick it up. targetPath must already exist
// (either a real yourql.db or one just created via CreateBlankDatabaseAt) —
// this function does not create files, it only repoints and restarts.
func SwitchActiveDatabase(targetPath string) error {
	if targetPath == "" {
		return fmt.Errorf("target path is empty")
	}
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("target database does not exist: %w", err)
	}

	ptr := models.ActiveDBPointer{Path: abs, UpdatedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize database pointer: %w", err)
	}
	pointerPath := models.ActiveDBPointerPath()
	if pointerPath == "" {
		return fmt.Errorf("could not determine pointer file location")
	}
	if err := os.WriteFile(pointerPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write database pointer: %w", err)
	}

	return restartApp()
}

// ClearActiveDatabasePointer removes the pointer file so the next launch
// falls back to the default ~/.yourql/yourql.db path.
func ClearActiveDatabasePointer() error {
	pointerPath := models.ActiveDBPointerPath()
	if pointerPath == "" {
		return fmt.Errorf("could not determine pointer file location")
	}
	if err := os.Remove(pointerPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear database pointer: %w", err)
	}
	return nil
}

// ResetToDefaultDatabase clears the pointer file (so the next launch uses
// ~/.yourql/yourql.db) and restarts the app to pick it up.
func ResetToDefaultDatabase() error {
	if err := ClearActiveDatabasePointer(); err != nil {
		return err
	}
	return restartApp()
}

// restartApp launches a fresh copy of the current executable and exits this
// process. Unlike the updater's detached-script pattern, no binary
// replacement is happening here — the database file is not the executable,
// so nothing prevents this process from starting the next process before
// exiting itself. This is simpler and has fewer moving parts than a shell
// script with a PID-wait loop.
//
// In development mode (wails dev), automatic restart is not possible because
// the app depends on a Vite dev server that is a child process of the Wails
// CLI — launching the binary directly orphans it from the dev environment.
// In that case the pointer file or clear has already succeeded; this
// function returns a message telling the user to restart manually.
func restartApp() error {
	if isDevMode() {
		return fmt.Errorf("database switch is configured — please restart the app manually to take effect (wails dev / quit and reopen)")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine running executable: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to relaunch: %w", err)
	}
	os.Exit(0)
	return nil // unreachable
}

// isDevMode returns true when the app is running under wails dev, where the
// frontend dev server is managed externally and the binary is launched with
// special flags/env vars.
func isDevMode() bool {
	if os.Getenv("frontenddevserverurl") != "" || os.Getenv("devserver") != "" {
		return true
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-frontenddevserverurl") || strings.HasPrefix(arg, "-devserver") {
			return true
		}
	}
	return false
}

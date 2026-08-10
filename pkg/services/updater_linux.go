//go:build linux

package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// updateExt is the file extension used for release assets on this OS.
const updateExt = ".AppImage"

// PerformUpgradeRestart writes a shell script that waits for the current
// process to exit, replaces the old AppImage with the new one, re-launches,
// and cleans up.
func PerformUpgradeRestart() error {
	entries, err := os.ReadDir(downloadDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("no staged update found — run DownloadUpdate first")
	}

	var imagePath string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".AppImage" {
			imagePath = filepath.Join(downloadDir, e.Name())
			break
		}
	}
	if imagePath == "" {
		return fmt.Errorf("no .AppImage found in staging directory")
	}

	// Determine the current AppImage path.
	runningPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find running AppImage path: %w", err)
	}

	// Write the upgrade script.
	script := filepath.Join(downloadDir, "upgrade.sh")
	scriptContent := fmt.Sprintf(`#!/bin/bash
# YourQL auto-upgrade script (Linux)
OLD_PID=%s
NEW_IMAGE="%s"
OLD_IMAGE="%s"
STAGING="%s"

# Wait for the old process to fully exit.
while kill -0 "$OLD_PID" 2>/dev/null; do
	sleep 0.5
done

# Replace the AppImage.
chmod +x "$NEW_IMAGE"
mv "$NEW_IMAGE" "$OLD_IMAGE"

# Relaunch.
nohup "$OLD_IMAGE" >/dev/null 2>&1 &

# Clean up staging directory.
rm -rf "$STAGING"

exit 0
`, strconv.Itoa(os.Getpid()), imagePath, runningPath, downloadDir)

	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write upgrade script: %w", err)
	}

	// Run the script detached.
	cmd := exec.Command("/bin/sh", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start upgrade script: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
}
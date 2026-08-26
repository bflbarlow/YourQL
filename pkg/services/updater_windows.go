//go:build windows

package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// updateExt is the file extension used for release assets on this OS.
const updateExt = ".exe"

// PerformUpgradeRestart detects the install mode, writes a PowerShell upgrade
// script, launches it detached, and exits the current process.
func PerformUpgradeRestart() error {
	entries, err := os.ReadDir(downloadDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("no staged update found — run DownloadUpdate first")
	}

	var exePath string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".exe") {
			exePath = filepath.Join(downloadDir, e.Name())
			break
		}
	}
	if exePath == "" {
		return fmt.Errorf("no .exe found in staging directory")
	}

	// Determine the current binary path.
	runningPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find running exe path: %w", err)
	}

	// Write a PowerShell upgrade script.
	script := filepath.Join(downloadDir, "upgrade.ps1")
	scriptContent := fmt.Sprintf(`# YourQL auto-upgrade script (Windows)
$oldPid = %s
$newExe = "%s"
$oldExe = "%s"
$stagingDir = "%s"

# Wait for old process to fully exit.
Wait-Process -Id $oldPid -ErrorAction SilentlyContinue

# Replace the binary.
Move-Item -Force $newExe $oldExe

# Relaunch.
Start-Process $oldExe

# Clean up staging directory.
Remove-Item -Recurse -Force $stagingDir -ErrorAction SilentlyContinue
`, strconv.Itoa(os.Getpid()),
		escapePowerShell(exePath),
		escapePowerShell(runningPath),
		escapePowerShell(downloadDir))

	// PowerShell requires UTF-16 for some configs; use UTF-8 with BOM.
	utf8bom := append([]byte{0xEF, 0xBB, 0xBF}, []byte(scriptContent)...)
	if err := os.WriteFile(script, utf8bom, 0644); err != nil {
		return fmt.Errorf("failed to write upgrade script: %w", err)
	}

	// Launch detached.
	cmd := exec.Command("powershell",
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", script)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start upgrade script: %w", err)
	}

	os.Exit(0)
	return nil // unreachable
}

// escapePowerShell escapes backslashes for PowerShell double-quoted strings.
func escapePowerShell(s string) string {
	return strings.ReplaceAll(s, `\`, `\\`)
}

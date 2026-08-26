//go:build darwin

package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// updateExt is the file extension used for release assets on this OS.
const updateExt = ".dmg"

// PerformUpgradeRestart executes the macOS upgrade flow:
//  1. Mounts the downloaded .dmg
//  2. Copies YourQL.app to a temp location preserving code signature (ditto)
//  3. Writes a shell script that waits for this process to exit, replaces the
//     app bundle, and relaunches
//  4. Launches the script in the background and exits the current process
func PerformUpgradeRestart() error {
	// Locate the downloaded .dmg.
	entries, err := os.ReadDir(downloadDir)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("no staged update found — run DownloadUpdate first")
	}

	var dmgPath string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".dmg" {
			dmgPath = filepath.Join(downloadDir, e.Name())
			break
		}
	}
	if dmgPath == "" {
		return fmt.Errorf("no .dmg found in staging directory")
	}

	// Mount the .dmg.
	mountPoint := filepath.Join(downloadDir, "mount")
	_ = os.MkdirAll(mountPoint, 0755)
	_ = exec.Command("hdiutil", "detach", "-quiet", mountPoint).Run() // unmount if stale

	attachCmd := exec.Command("hdiutil", "attach", "-nobrowse", "-quiet",
		"-mountpoint", mountPoint, dmgPath)
	if out, err := attachCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to mount .dmg: %w (%s)", err, string(out))
	}

	defer func() {
		_ = exec.Command("hdiutil", "detach", "-quiet", mountPoint).Run()
	}()

	// Find the .app bundle inside the mounted volume.
	appEntries, err := os.ReadDir(mountPoint)
	if err != nil {
		return fmt.Errorf("failed to read mounted .dmg: %w", err)
	}

	var appName string
	for _, e := range appEntries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".app" {
			appName = e.Name()
			break
		}
		// Some .dmgs have a symlink to /Applications — skip it.
	}
	if appName == "" {
		return fmt.Errorf("no .app bundle found in .dmg")
	}

	// Copy the .app to the staging dir using ditto (preserves code signature
	// and extended attributes).
	stagedApp := filepath.Join(downloadDir, appName)
	_ = os.RemoveAll(stagedApp)

	ditto := exec.Command("ditto", "--rsrc",
		filepath.Join(mountPoint, appName), stagedApp)
	if out, err := ditto.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to copy app bundle: %w (%s)", err, string(out))
	}

	// Determine the running app's bundle path (handle non-/Applications installs).
	runningAppPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to find running app path: %w", err)
	}

	// Walk up the path to find the .app root.
	for p := runningAppPath; p != "/" && p != "."; p = filepath.Dir(p) {
		if filepath.Ext(p) == ".app" {
			runningAppPath = p
			break
		}
	}

	// Write the upgrade script.
	script := filepath.Join(downloadDir, "upgrade.sh")
	scriptContent := fmt.Sprintf(`#!/bin/bash
# YourQL auto-upgrade script (macOS)
# Waits for the old process to exit, replaces the app, and relaunches.

OLD_PID=%s
OLD_APP="%s"
NEW_APP="%s"

# Wait for the old process to fully exit.
while kill -0 "$OLD_PID" 2>/dev/null; do
	sleep 0.5
done

# Replace the app bundle.
rm -rf "$OLD_APP"
mv "$NEW_APP" "$OLD_APP"

# Relaunch.
open "$OLD_APP"

# Clean up the staging directory.
rm -rf "%s"
`, strconv.Itoa(os.Getpid()),
		runningAppPath,
		stagedApp,
		downloadDir)

	if err := os.WriteFile(script, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write upgrade script: %w", err)
	}

	// Run the script in the background and exit.
	cmd := exec.Command("/bin/sh", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start upgrade script: %w", err)
	}

	// Exit the process so the script's while loop unblocks quickly.
	os.Exit(0)
	return nil // unreachable
}

package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ---------------------------------------------------------------------------
// Types (repeated from app.go for the service layer; kept in sync manually)
// ---------------------------------------------------------------------------

// UpdateInfo describes a discovered update.
type UpdateInfo struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseNotes    string `json:"release_notes"`
	DownloadURL     string `json:"download_url"`
	AssetChecksum   string `json:"asset_checksum"`
	PublishedAt     string `json:"published_at"`
}

// GitHub release API response (only the fields we need).
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// ---------------------------------------------------------------------------
// Asset detection
// ---------------------------------------------------------------------------

func assetNameForOS() string {
	switch runtime.GOOS {
	case "darwin":
		return ".dmg"
	case "windows":
		return ".exe"
	case "linux":
		return ".AppImage"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// CheckForUpdate
// ---------------------------------------------------------------------------

// CheckForUpdate queries the GitHub Releases API and compares the latest tag
// against the running version. If the app was built without a release tag
// (appVersion is "0.4.0"), it returns immediately — dev builds should not
// self-update. Production builds inject the real version via ldflags.
func CheckForUpdate(appVersion string) (*UpdateInfo, error) {
	info := &UpdateInfo{CurrentVersion: appVersion}

	if appVersion == "dev" || appVersion == "0.4.0" {
		return info, nil
	}

	// Parse the running version so we can compare.
	current, err := semver.NewVersion(strings.TrimPrefix(appVersion, "v"))
	if err != nil {
		// Can't parse our own version — bail out gracefully.
		return info, nil
	}

	// Fetch latest release from GitHub.
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, err
	}

	latestTag := release.TagName
	latest, err := semver.NewVersion(strings.TrimPrefix(latestTag, "v"))
	if err != nil {
		// Can't parse GitHub tag — assume we're up to date so we don't nag.
		return info, nil
	}

	info.LatestVersion = latestTag
	info.ReleaseNotes = release.Body
	info.PublishedAt = release.PublishedAt
	info.UpdateAvailable = latest.GreaterThan(current)

	if !info.UpdateAvailable {
		return info, nil
	}

	// Find the correct asset for this OS.
	assetSuffix := assetNameForOS()
	if assetSuffix == "" {
		return info, nil // unsupported OS
	}

	// Also get the checksums file.
	var checksumsURL string
	for _, a := range release.Assets {
		if a.Name == "checksums.txt" {
			checksumsURL = a.BrowserDownloadURL
		}
		if strings.HasSuffix(strings.ToLower(a.Name), strings.ToLower(assetSuffix)) {
			info.DownloadURL = a.BrowserDownloadURL
		}
	}

	if info.DownloadURL == "" {
		return nil, fmt.Errorf("no %s asset found in latest release", assetSuffix)
	}

	// Resolve the expected checksum for the asset we'll download.
	if checksumsURL != "" {
		assetName := filepath.Base(info.DownloadURL)
		checksum, err := fetchChecksum(checksumsURL, assetName)
		if err == nil {
			info.AssetChecksum = checksum
		}
		// If checksums.txt is missing or we can't parse the line,
		// AssetChecksum stays "" and DownloadUpdate will refuse to proceed.
	}

	return info, nil
}

func fetchLatestRelease() (*ghRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/bflbarlow/YourQL/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("failed to reach update server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update server returned HTTP %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &release, nil
}

func fetchChecksum(checksumsURL, assetName string) (string, error) {
	resp, err := http.Get(checksumsURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s in checksums.txt", assetName)
}

// ---------------------------------------------------------------------------
// DownloadUpdate
// ---------------------------------------------------------------------------

// downloadDir is the temp directory used for staging updates.
var downloadDir = filepath.Join(os.TempDir(), "yourql-update")

// DownloadUpdate fetches the asset at downloadURL, verifies it against
// expectedSHA256, and stages it for installation. Returns an error if any
// step fails — the caller should surface the error and not proceed to restart.
func DownloadUpdate(downloadURL, expectedSHA256 string) error {
	if expectedSHA256 == "" {
		return fmt.Errorf("no checksum provided — refusing to download unverified binary")
	}

	// Ensure staging directory exists.
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}

	assetName := filepath.Base(downloadURL)
	partial := filepath.Join(downloadDir, assetName+".download")

	// 1. Download to a temp file.
	f, err := os.Create(partial)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		_ = f.Close()
		_ = os.Remove(partial)
		return fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = f.Close()
		_ = os.Remove(partial)
		return fmt.Errorf("download server returned HTTP %d", resp.StatusCode)
	}

	_, err = io.Copy(f, resp.Body)
	_ = f.Close()
	if err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("failed to save update: %w", err)
	}

	// 2. Compute SHA256.
	fileBytes, err := os.ReadFile(partial)
	if err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("failed to read downloaded file: %w", err)
	}
	actualHash := sha256.Sum256(fileBytes)
	actualHex := hex.EncodeToString(actualHash[:])

	// 3. Compare against expected.
	if !strings.EqualFold(actualHex, expectedSHA256) {
		_ = os.Remove(partial)
		return fmt.Errorf("checksum mismatch — downloaded file failed verification; expected %s, got %s", expectedSHA256, actualHex)
	}

	// 4. Rename to final name (still in the staging dir).
	final := filepath.Join(downloadDir, assetName)
	_ = os.Remove(final) // in case a stale file is there
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return fmt.Errorf("failed to stage update: %w", err)
	}

	return nil
}

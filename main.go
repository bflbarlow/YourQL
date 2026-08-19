package main

import (
	"embed"
	"flag"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"YourQL/pkg/models"

	"github.com/joho/godotenv"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

// appVersion is set at build time via -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)".
// Falls back to "dev" for local development (wails dev) and signals the updater to skip itself.
var appVersion = "dev"

// logFile holds the handle to the diagnostic log file (nil when logging is disabled).
var logFile *os.File
var logFileMu sync.Mutex

// setupLogging configures diagnostic file logging when enabled in app_settings.
// Must be called after models.ConnectDatabase() so the settings table is available.
func setupLogging() {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	enabled, err := getLoggingEnabledFromDB()
	if err != nil {
		slog.Warn("could not read logging setting, defaulting to off", "error", err)
		return
	}
	if !enabled {
		return
	}

	dir := filepath.Join(os.Getenv("HOME"), ".yourql")
	if err := os.MkdirAll(dir, 0700); err != nil {
		slog.Warn("could not create .yourql directory for logging", "error", err)
		return
	}

	logPath := filepath.Join(dir, "yourql.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		slog.Warn("could not open log file", "path", logPath, "error", err)
		return
	}

	logFile = f

	// Redirect both log and slog to stderr + file
	mw := io.MultiWriter(os.Stderr, f)
	log.SetOutput(mw)
	slog.SetDefault(slog.New(slog.NewTextHandler(mw, nil)))

	slog.Info("diagnostic logging enabled", "path", logPath)
}

// getLoggingEnabledFromDB reads the logging_enabled setting directly from the
// app's SQLite database. This is used during startup, before the services layer
// is fully initialized.
func getLoggingEnabledFromDB() (bool, error) {
	if models.DB == nil {
		return false, nil
	}
	var value string
	err := models.DB.QueryRow(
		"SELECT value FROM app_settings WHERE key = 'logging_enabled'",
	).Scan(&value)
	if err != nil {
		return false, nil // no rows or error → default to off
	}
	return value == "true", nil
}

func main() {
	// Headless flags (--headless, --port, --db-path).
	// Off by default — every existing launch path (wails dev, wails build,
	// release binaries) is unchanged.
	headlessFlag := flag.Bool("headless", false, "run headless HTTP server instead of the GUI")
	portFlag := flag.Int("port", 8745, "headless HTTP port (localhost only)")
	dbPathFlag := flag.String("db-path", "", "explicit app database path for headless mode")
	flag.Parse()

	// Load .env file if present (ignored if not found)
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, skipping")
	}

	if *headlessFlag {
		if err := runHeadless(*portFlag, *dbPathFlag); err != nil {
			slog.Error("headless error", "error", err)
			os.Exit(1)
		}
		return
	}

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "YourQL",
		Width:  1024,
		Height: 1152,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Mac: &mac.Options{
			DisableZoom: false,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		slog.Error("application error", "error", err)
	}
}

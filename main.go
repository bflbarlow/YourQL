package main

import (
	"embed"
	"log/slog"

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
var appVersion = "0.4.0"

func main() {
	// Load .env file if present (ignored if not found)
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, skipping")
	}

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "YourQL",
		Width:  1024,
		Height: 768,
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

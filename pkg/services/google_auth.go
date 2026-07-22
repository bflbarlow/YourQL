package services

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"YourQL/pkg/models"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/sheets/v4"
)

// Default Google OAuth2 client ID (bundled with the app).
// Users can override via YOURQL_GOOGLE_CLIENT_ID / YOURQL_GOOGLE_CLIENT_SECRET env vars.
const defaultGoogleClientID = ""
const defaultGoogleClientSecret = ""

// Google OAuth2 scopes — read-only access to spreadsheets.
var googleSheetsScopes = []string{
	"https://www.googleapis.com/auth/spreadsheets.readonly",
}

// googleOAuthConfig builds the OAuth2 config using bundled or env-overridden credentials.
func googleOAuthConfig() *oauth2.Config {
	clientID := defaultGoogleClientID
	clientSecret := defaultGoogleClientSecret
	if v := os.Getenv("YOURQL_GOOGLE_CLIENT_ID"); v != "" {
		clientID = v
	}
	if v := os.Getenv("YOURQL_GOOGLE_CLIENT_SECRET"); v != "" {
		clientSecret = v
	}
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       googleSheetsScopes,
	}
}

// ============================================================
// Loopback Redirect Flow (for Desktop app clients)
// ============================================================

// LoopbackResult is returned by StartLoopbackServer after the user authorizes.
type LoopbackResult struct {
	Token *oauth2.Token
	Error error
}

// StartLoopbackServer starts an HTTP server on localhost:0 and returns the
// authorization URL to open in the browser. The resultCh receives the token
// (or error) after the user completes the OAuth flow.
func StartLoopbackServer(ctx context.Context) (authURL string, resultCh <-chan LoopbackResult, err error) {
	cfg := googleOAuthConfig()

	if cfg.ClientID == "" {
		return "", nil, fmt.Errorf("Google OAuth credentials not configured. Set YOURQL_GOOGLE_CLIENT_ID and YOURQL_GOOGLE_CLIENT_SECRET environment variables.\n\nGet credentials at: https://console.cloud.google.com/apis/credentials")
	}

	// Find a free port on localhost
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("failed to start auth server: %w", err)
	}

	// Generate a random state token for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		listener.Close()
		return "", nil, fmt.Errorf("failed to generate state: %w", err)
	}
	state := hex.EncodeToString(stateBytes)

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)
	cfg.RedirectURL = redirectURI

	// Build the auth URL — requires refresh_token
	authURL = cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	ch := make(chan LoopbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// Verify state to prevent CSRF
		if q.Get("state") != state {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`<html><body><h1>Authentication Failed</h1><p>Invalid state parameter. This could be a CSRF attack.</p></body></html>`))
			ch <- LoopbackResult{Error: fmt.Errorf("oauth state mismatch")}
			return
		}

		code := q.Get("code")
		if code == "" {
			errMsg := q.Get("error")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `<html><body><h1>Authentication Failed</h1><p>%s</p></body></html>`, errMsg)
			ch <- LoopbackResult{Error: fmt.Errorf("oauth error: %s", errMsg)}
			return
		}

		// Exchange authorization code for token
		tok, err := cfg.Exchange(ctx, code)
		if err != nil {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `<html><body><h1>Authentication Failed</h1><p>Token exchange error: %s</p></body></html>`, err)
			ch <- LoopbackResult{Error: fmt.Errorf("token exchange failed: %w", err)}
			return
		}

		// Success page
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head><style>body{font-family:-apple-system,BlinkMacSystemFont,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#fff;color:#222}div{text-align:center}h1{font-size:1.5rem;color:#333}p{color:#666}</style></head>
<body><div><h1>✓ Connected</h1><p>You can close this tab and return to YourQL.</p></div></body></html>`))
		ch <- LoopbackResult{Token: tok}
		go server.Close() // shut down after serving
	})

	go func() {
		server.Serve(listener)
	}()

	// Shutdown the server if the context is cancelled (app exits)
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	return authURL, ch, nil
}

// ============================================================
// Token Storage & Refresh
// ============================================================

// StoreAuthConfig serializes an OAuth2 token into the data source's auth_config column.
func StoreAuthConfig(connID uint, token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	_, err = models.DB.Exec(
		"UPDATE data_sources SET auth_config = ? WHERE id = ?",
		string(data), connID,
	)
	return err
}

// LoadAuthConfig deserializes the OAuth2 token from the data source's auth_config column.
func LoadAuthConfig(connID uint) (*oauth2.Token, error) {
	var raw sql.NullString
	err := models.DB.QueryRow(
		"SELECT auth_config FROM data_sources WHERE id = ?", connID,
	).Scan(&raw)
	if err != nil || !raw.Valid || raw.String == "" {
		return nil, fmt.Errorf("no auth config for data source %d", connID)
	}
	var tok oauth2.Token
	if err := json.Unmarshal([]byte(raw.String), &tok); err != nil {
		return nil, fmt.Errorf("failed to parse auth config: %w", err)
	}
	return &tok, nil
}

// getSheetsClient returns an authenticated Google Sheets API client for a data source.
func getSheetsClient(conn *models.DataSource) (*sheets.Service, error) {
	tok, err := LoadAuthConfig(conn.ID)
	if err != nil {
		return nil, err
	}

	if tok.Expiry.Before(time.Now()) {
		if tok.RefreshToken == "" {
			return nil, fmt.Errorf("oauth_token_expired")
		}
	}

	cfg := googleOAuthConfig()
	client := cfg.Client(context.Background(), tok)
	return sheets.New(client)
}

// ============================================================
// Token Revocation
// ============================================================

// ClearAuthConfig removes OAuth tokens for a data source.
func ClearAuthConfig(connID uint) error {
	_, err := models.DB.Exec(
		"UPDATE data_sources SET auth_config = NULL WHERE id = ?", connID,
	)
	return err
}

// RevokeToken revokes the access token with Google's revocation endpoint.
func RevokeToken(token *oauth2.Token) error {
	if token.AccessToken == "" {
		return nil
	}
	resp, err := http.PostForm("https://oauth2.googleapis.com/revoke",
		url.Values{"token": {token.AccessToken}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("revocation failed: %d", resp.StatusCode)
	}
	return nil
}

// ============================================================
// Temp Session Storage (for unsaved connections)
// ============================================================

var (
	tempTokensMu sync.Mutex
	tempTokens   = make(map[string]*oauth2.Token)
)

// StoreTempAuthConfig stores an OAuth token in memory keyed by a temp session ID.
func StoreTempAuthConfig(sessionID string, token *oauth2.Token) {
	tempTokensMu.Lock()
	defer tempTokensMu.Unlock()
	tempTokens[sessionID] = token
}

// LoadTempAuthConfig retrieves an OAuth token from temp storage.
func LoadTempAuthConfig(sessionID string) (*oauth2.Token, error) {
	tempTokensMu.Lock()
	defer tempTokensMu.Unlock()
	tok, ok := tempTokens[sessionID]
	if !ok {
		return nil, fmt.Errorf("no temp auth config for session %s", sessionID)
	}
	return tok, nil
}

// MigrateTempAuthToDB moves the temp-stored OAuth token into the real DB record.
func MigrateTempAuthToDB(sessionID string, connID uint) error {
	tok, err := LoadTempAuthConfig(sessionID)
	if err != nil {
		return err
	}
	if err := StoreAuthConfig(connID, tok); err != nil {
		return err
	}
	tempTokensMu.Lock()
	delete(tempTokens, sessionID)
	tempTokensMu.Unlock()
	return nil
}
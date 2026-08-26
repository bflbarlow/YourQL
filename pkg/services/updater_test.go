package services

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Updater verification suite (TECH_REVIEW_20260824.md F-4 / P3). These lock
// in the AGENT_READ_FIRST.md §3.8 rules: no download without a checksum, no
// staged artifact unless SHA256 matches exactly.

func withTestDownloadDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := downloadDir
	downloadDir = dir
	t.Cleanup(func() { downloadDir = old })
	return dir
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestDownloadUpdate_RefusesEmptyChecksum(t *testing.T) {
	withTestDownloadDir(t)

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("payload"))
	}))
	defer srv.Close()

	if err := DownloadUpdate(srv.URL+"/yourql", ""); err == nil {
		t.Fatal("empty checksum must be refused")
	}
	if called {
		t.Error("refusal must happen before any network call")
	}
}

func TestDownloadUpdate_RefusesChecksumMismatch(t *testing.T) {
	dir := withTestDownloadDir(t)

	payload := []byte("fake binary payload v1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	err := DownloadUpdate(srv.URL+"/yourql", sha256hex([]byte("tampered payload")))
	if err == nil {
		t.Fatal("checksum mismatch must be refused")
	}

	// Nothing runnable/staged may remain after a failed verification.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".download" {
			t.Errorf("staged artifact %q must not survive failed verification", e.Name())
		}
	}
}

func TestDownloadUpdate_AcceptsMatchingChecksum(t *testing.T) {
	dir := withTestDownloadDir(t)

	payload := []byte("#!/bin/sh\necho yourql\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	if err := DownloadUpdate(srv.URL+"/yourql", sha256hex(payload)); err != nil {
		t.Fatalf("matching checksum should stage the update: %v", err)
	}
	staged := filepath.Join(dir, "yourql")
	got, err := os.ReadFile(staged)
	if err != nil {
		t.Fatalf("staged artifact missing: %v", err)
	}
	if string(got) != string(payload) {
		t.Error("staged artifact content differs from downloaded payload")
	}
}

func TestDownloadUpdate_RefusesHTTPErrorStatus(t *testing.T) {
	withTestDownloadDir(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	if err := DownloadUpdate(srv.URL+"/yourql", sha256hex([]byte("anything"))); err == nil {
		t.Fatal("HTTP error status must be refused")
	}
}

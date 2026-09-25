//go:build integration

package main

// An actual PTY round-trip test. Build with:
//   go test -tags integration -v ./...
//
// Skipped by default so `go test ./...` doesn't spawn shells in CI.

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegration_PTYEcho(t *testing.T) {
	b := &unixPtyBackend{}
	h, err := b.Start(80, 24, "/bin/sh", []string{}, os.TempDir(), os.Environ())
	if err != nil {
		t.Skipf("PTY not available: %v", err)
	}
	defer h.Close()

	// Read the prompt. Then send `echo hello\n` and read the echo back.
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	var got bytes.Buffer
	for time.Now().Before(deadline) && got.Len() < 4096 {
		h.Resize(80, 24)
		// Set non-blocking-ish read by polling
		set := false
		_ = set
		n, _ := h.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Type the command.
	cmd := "echo hello-spt\n"
	if _, err := h.Write([]byte(cmd)); err != nil {
		t.Fatal(err)
	}

	// Wait for the echo back.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, _ := h.Read(buf)
		if n > 0 {
			got.Write(buf[:n])
		}
		if bytes.Contains(got.Bytes(), []byte("hello-spt")) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !bytes.Contains(got.Bytes(), []byte("hello-spt")) {
		t.Fatalf("did not see echoed command. got: %q", got.String())
	}

	// Also confirm we can round-trip base64.
	enc := base64.StdEncoding.EncodeToString([]byte("hello-spt\n"))
	dec, _ := base64.StdEncoding.DecodeString(enc)
	if string(dec) != "hello-spt\n" {
		t.Fatalf("base64")
	}

	_ = strings.TrimSpace
}

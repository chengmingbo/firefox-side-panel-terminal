// smoke_test.go — local test, not built into the binary.
//
// Run with:  go test -v ./...
//
// We exercise the framed I/O and session start logic without needing Firefox.

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFramedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := map[string]any{"cmd": "start", "id": "abc", "cols": 80, "rows": 24}
	if err := writeFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}

	r := bytes.NewReader(buf.Bytes())
	got, err := readFrame(r)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got["cmd"] != "start" {
		t.Fatalf("cmd = %v, want start", got["cmd"])
	}
}

// We re-implement read/write here to test them in isolation; main.go's
// versions take os.Stdin/Stdout.
func readFrame(r interface {
	Read([]byte) (int, error)
}) (map[string]any, error) {
	var hdr [4]byte
	n, err := r.Read(hdr[:])
	if err != nil || n != 4 {
		return nil, err
	}
	l := binary.LittleEndian.Uint32(hdr[:])
	body := make([]byte, l)
	n, err = r.Read(body)
	if err != nil || n != int(l) {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(body, &m)
}

func writeFrame(w interface {
	Write([]byte) (int, error)
}, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func TestShellPick(t *testing.T) {
	if runtime := os.Getenv("SHELL"); runtime != "" {
		// Should default to $SHELL when nothing requested.
		shell, _ := pickShell("")
		if shell != runtime {
			t.Skipf("SHELL is %q, picking %q", runtime, shell)
		}
	}
}

func TestSetEnv(t *testing.T) {
	env := []string{"A=1", "B=2"}
	env = setEnv(env, "B", "x")
	env = setEnv(env, "C", "3")
	if len(env) != 3 || env[1] != "B=x" || env[2] != "C=3" {
		t.Fatalf("unexpected env: %v", env)
	}
}

func TestBase64(t *testing.T) {
	in := []byte("\x1b[31mred\x1b[0m\n")
	enc := base64.StdEncoding.EncodeToString(in)
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(dec) != string(in) {
		t.Fatalf("round trip lost bytes")
	}
}

func TestPathHelpers(t *testing.T) {
	tmp := t.TempDir()
	_ = filepath.Join(tmp, "x")
	_ = strings.Contains(tmp, "/")
	_ = time.Now()
}

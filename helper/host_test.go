package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestFramedRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := command{Cmd: "start", ID: "abc", Cols: 80, Rows: 24, Env: map[string]string{"A": "1"}}
	if err := writeFramedMessage(&buf, want); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(buf.Bytes()); int(got) != buf.Len()-4 {
		t.Fatalf("header length %d, body %d", got, buf.Len()-4)
	}
	var got command
	if err := readFramedMessage(&buf, &got); err != nil {
		t.Fatalf("readFramedMessage: %v", err)
	}
	if got.Cmd != "start" || got.ID != "abc" || got.Cols != 80 || got.Env["A"] != "1" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestReadFramedMessageRejectsBadLength(t *testing.T) {
	for _, n := range []uint32{0, maxInboundFrame + 1} {
		var hdr [4]byte
		binary.LittleEndian.PutUint32(hdr[:], n)
		var c command
		if err := readFramedMessage(bytes.NewReader(hdr[:]), &c); err == nil {
			t.Fatalf("length %d accepted", n)
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

func TestPickShellIsLogin(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	if shell, args := pickShell(""); shell != "/bin/zsh" || len(args) != 1 || args[0] != "-l" {
		t.Fatalf("pickShell(\"\") = %q %v", shell, args)
	}
	if shell, _ := pickShell(" /bin/sh "); shell != "/bin/sh" {
		t.Fatalf("explicit shell not honoured: %q", shell)
	}
}

func TestClampDims(t *testing.T) {
	if c, r := clampDims(0, -1); c != 100 || r != 28 {
		t.Fatalf("defaults: %d %d", c, r)
	}
	if c, r := clampDims(99999, 99999); c != maxDim || r != maxDim {
		t.Fatalf("cap: %d %d", c, r)
	}
}

// frameReader decodes helper → browser frames from a pipe.
type frameReader struct{ r io.Reader }

type event struct {
	Evt     string `json:"evt"`
	ID      string `json:"id"`
	Data    string `json:"data"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (f frameReader) next(t *testing.T) event {
	t.Helper()
	var e event
	if err := readFramedMessage(f.r, &e); err != nil {
		t.Fatalf("reading event: %v", err)
	}
	return e
}

func TestPing(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go newHost(nil, outW).run(inR)
	defer inW.Close()
	go writeFramedMessage(inW, command{Cmd: "ping"})
	var e struct{ Evt, Version string }
	if err := readFramedMessage(outR, &e); err != nil || e.Evt != "pong" || e.Version != version {
		t.Fatalf("ping: %+v %v", e, err)
	}
}

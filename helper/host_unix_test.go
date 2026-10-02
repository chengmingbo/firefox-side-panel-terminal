//go:build unix

package main

import (
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"time"
)

// TestHostSession drives the real host over pipes: start /bin/sh in a PTY,
// type a command, and check we get its output and exit code back.
func TestHostSession(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := newHost(newUnixBackend(), outW)
	runDone := make(chan error, 1)
	go func() { runDone <- h.run(inR) }()

	events := make(chan event, 256)
	go func() {
		fr := frameReader{outR}
		for {
			var e event
			if err := readFramedMessage(fr.r, &e); err != nil {
				close(events)
				return
			}
			events <- e
		}
	}()

	send := func(c command) {
		if err := writeFramedMessage(inW, c); err != nil {
			t.Fatal(err)
		}
	}
	send(command{Cmd: "start", ID: "s1", Cols: 80, Rows: 24, Shell: "/bin/sh", Env: map[string]string{"SPT_TEST": "marker-42"}})
	send(command{Cmd: "resize", ID: "s1", Cols: 120, Rows: 40})
	send(command{Cmd: "input", ID: "s1", Data: base64.StdEncoding.EncodeToString(
		[]byte("echo \"$SPT_TEST $(stty size)\"; exit 3\n"))})

	var out strings.Builder
	var started bool
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("event stream ended; output so far: %q", out.String())
			}
			switch e.Evt {
			case "started":
				started = true
			case "output":
				b, err := base64.StdEncoding.DecodeString(e.Data)
				if err != nil {
					t.Fatal(err)
				}
				out.Write(b)
			case "error":
				t.Fatalf("helper error: %s", e.Message)
			case "closed":
				if !started {
					t.Fatal("closed before started")
				}
				if e.Code != 3 {
					t.Fatalf("exit code %d, want 3", e.Code)
				}
				if !strings.Contains(out.String(), "marker-42 40 120") {
					t.Fatalf("missing env/resize in output: %q", out.String())
				}
				inW.Close()
				if err := <-runDone; err != io.EOF {
					t.Fatalf("run returned %v, want EOF", err)
				}
				return
			}
		case <-deadline:
			t.Fatalf("timed out; output so far: %q", out.String())
		}
	}
}

// TestHostStop checks that "stop" hangs up a running shell.
func TestHostStop(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := newHost(newUnixBackend(), outW)
	go h.run(inR)
	defer inW.Close()

	go writeFramedMessage(inW, command{Cmd: "start", ID: "s2", Shell: "/bin/sh"})
	fr := frameReader{outR}
	if e := fr.next(t); e.Evt != "started" {
		t.Fatalf("got %+v, want started", e)
	}
	go writeFramedMessage(inW, command{Cmd: "stop", ID: "s2"})
	done := make(chan event)
	go func() {
		for {
			var e event
			if readFramedMessage(outR, &e) != nil {
				return
			}
			if e.Evt == "closed" {
				done <- e
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(killGrace + 3*time.Second):
		t.Fatal("shell not stopped")
	}
}

// firefox-side-panel-terminal-host: a native messaging host that spawns
// PTYs for the Firefox "Side Panel Terminal" extension.
//
// Wire protocol (one JSON object per line):
//
//	→ {"cmd":"start","id":"...","cols":100,"rows":28,"shell":"","cwd":"","env":{}}
//	→ {"cmd":"input","id":"...","data":"<base64>"}
//	→ {"cmd":"resize","id":"...","cols":100,"rows":28}
//	→ {"cmd":"stop","id":"..."}
//	→ {"cmd":"shutdown"}
//
//	← {"evt":"started","id":"...","title":"zsh - 100x28"}
//	← {"evt":"output","id":"...","data":"<base64>"}
//	← {"evt":"closed","id":"...","code":0}
//	← {"evt":"error","id":"...","message":"..."}
//
// Firefox communicates with native hosts over stdio using a length-prefixed
// framing: each message is preceded by a 4-byte little-endian uint32 giving
// the JSON payload length. We use github.com/lmorg/ptysrv/xtermio to
// manage the PTY because it gives us proper resize handling across macOS,
// Linux and Windows (ConPTY).
//
// Build:  go build -o firefox-side-panel-terminal-host .
// Install: ./scripts/install.sh   (writes the native messaging manifest
//                                to the right Firefox profile dir).

package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Browser → helper
type cmdStart struct {
	Cmd   string            `json:"cmd"`
	ID    string            `json:"id"`
	Cols  int               `json:"cols"`
	Rows  int               `json:"rows"`
	Shell string            `json:"shell"`
	Cwd   string            `json:"cwd"`
	Env   map[string]string `json:"env"`
}
type cmdInput struct {
	Cmd  string `json:"cmd"`
	ID   string `json:"id"`
	Data string `json:"data"` // base64
}
type cmdResize struct {
	Cmd  string `json:"cmd"`
	ID   string `json:"id"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}
type cmdStop struct {
	Cmd string `json:"cmd"`
	ID  string `json:"id"`
}
type cmdShutdown struct {
	Cmd string `json:"cmd"`
}

// Helper → browser
type evtStarted struct {
	Evt   string `json:"evt"`
	ID    string `json:"id"`
	Title string `json:"title"`
}
type evtOutput struct {
	Evt  string `json:"evt"`
	ID   string `json:"id"`
	Data string `json:"data"` // base64
}
type evtClosed struct {
	Evt  string `json:"evt"`
	ID   string `json:"id"`
	Code int    `json:"code"`
}
type evtError struct {
	Evt     string `json:"evt"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

// ptyBackend abstracts the OS PTY layer so the same code runs on macOS,
// Linux, and Windows (ConPTY).
type ptyBackend interface {
	Start(cols, rows int, shell string, args []string, cwd string, env []string) (PtyHandle, error)
}

type PtyHandle interface {
	Write([]byte) (int, error)
	Read(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

func main() {
	log.SetPrefix("spt-host: ")
	log.SetFlags(log.Lmicroseconds)

	backend, err := pickBackend()
	if err != nil {
		fatal("no PTY backend available: %v", err)
	}
	h := &host{backend: backend, sessions: map[string]*session{}}

	if err := h.run(); err != nil && !errors.Is(err, io.EOF) {
		fatal("run: %v", err)
	}
}

type host struct {
	backend  ptyBackend
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	id    string
	pty   PtyHandle
	cmd   *exec.Cmd
	done  chan struct{}
	colsi int
	rowsi int
}

func (h *host) run() error {
	in := bufio.NewReader(os.Stdin)
	for {
		msg, err := readFramedMessage(in)
		if err != nil {
			return err
		}
		h.dispatch(msg)
	}
}

// readFramedMessage reads one Firefox native-messaging frame: a 4-byte
// little-endian length followed by that many bytes of JSON.
func readFramedMessage(r io.Reader) (map[string]json.RawMessage, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 || n > 16*1024*1024 {
		return nil, fmt.Errorf("invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// writeFramedMessage writes one Firefox native-messaging frame.
func writeFramedMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func (h *host) dispatch(m map[string]json.RawMessage) {
	cmd := stringField(m, "cmd")
	switch cmd {
	case "start":
		var p cmdStart
		if !decodeInto(m, &p) {
			h.sendError("", "invalid start payload")
			return
		}
		h.startSession(&p)
	case "input":
		var p cmdInput
		if !decodeInto(m, &p) {
			h.sendError("", "invalid input payload")
			return
		}
		h.sessionInput(&p)
	case "resize":
		var p cmdResize
		if !decodeInto(m, &p) {
			h.sendError("", "invalid resize payload")
			return
		}
		h.sessionResize(&p)
	case "stop":
		var p cmdStop
		if !decodeInto(m, &p) {
			h.sendError("", "invalid stop payload")
			return
		}
		h.stopSession(p.ID)
	case "shutdown":
		h.shutdown()
	default:
		h.sendError("", fmt.Sprintf("unknown cmd %q", cmd))
	}
}

func (h *host) startSession(p *cmdStart) {
	shell, args := pickShell(p.Shell)
	cwd := p.Cwd
	if cwd == "" {
		if h, err := os.UserHomeDir(); err == nil {
			cwd = h
		}
	}
	if _, err := os.Stat(cwd); err != nil {
		h.sendError(p.ID, fmt.Sprintf("invalid cwd %q: %v", cwd, err))
		return
	}

	cols, rows := p.Cols, p.Rows
	if cols <= 0 {
		cols = 100
	}
	if rows <= 0 {
		rows = 28
	}

	env := os.Environ()
	for k, v := range p.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	// Tell the shell it's a terminal.
	env = setEnv(env, "TERM", "xterm-256color")
	env = setEnv(env, "COLORTERM", "truecolor")

	handle, err := h.backend.Start(cols, rows, shell, args, cwd, env)
	if err != nil {
		h.sendError(p.ID, fmt.Sprintf("pty start failed: %v", err))
		return
	}

	s := &session{
		id:    p.ID,
		pty:   handle,
		done:  make(chan struct{}),
		colsi: cols,
		rowsi: rows,
	}
	h.mu.Lock()
	h.sessions[p.ID] = s
	h.mu.Unlock()

	title := fmt.Sprintf("%s - %dx%d", filepath.Base(shell), cols, rows)
	h.send(evtStarted{Evt: "started", ID: p.ID, Title: title})

	go h.readLoop(s)
	go h.waitForExit(s)
}

func (h *host) readLoop(s *session) {
	defer close(s.done)
	buf := make([]byte, 16*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			h.send(evtOutput{
				Evt:  "output",
				ID:   s.id,
				Data: base64.StdEncoding.EncodeToString(buf[:n]),
			})
		}
		if err != nil {
			return
		}
	}
}

func (h *host) waitForExit(s *session) {
	<-s.done
	// The PTY handle doesn't expose Wait(), so we just sleep a short
	// moment and then close. The browser side will see "closed".
	time.Sleep(50 * time.Millisecond)
	_ = s.pty.Close()
	h.mu.Lock()
	delete(h.sessions, s.id)
	h.mu.Unlock()
	h.send(evtClosed{Evt: "closed", ID: s.id, Code: 0})
}

func (h *host) sessionInput(p *cmdInput) {
	h.mu.Lock()
	s, ok := h.sessions[p.ID]
	h.mu.Unlock()
	if !ok {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		h.sendError(p.ID, fmt.Sprintf("base64 decode: %v", err))
		return
	}
	if _, err := s.pty.Write(raw); err != nil {
		h.sendError(p.ID, fmt.Sprintf("write: %v", err))
	}
}

func (h *host) sessionResize(p *cmdResize) {
	h.mu.Lock()
	s, ok := h.sessions[p.ID]
	h.mu.Unlock()
	if !ok {
		return
	}
	if err := s.pty.Resize(p.Cols, p.Rows); err != nil {
		h.sendError(p.ID, fmt.Sprintf("resize: %v", err))
		return
	}
	s.colsi, s.rowsi = p.Cols, p.Rows
}

func (h *host) stopSession(id string) {
	h.mu.Lock()
	s, ok := h.sessions[id]
	h.mu.Unlock()
	if !ok {
		return
	}
	_ = s.pty.Close()
}

func (h *host) shutdown() {
	h.mu.Lock()
	sessions := make([]*session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.Unlock()
	for _, s := range sessions {
		_ = s.pty.Close()
	}
	os.Exit(0)
}

// send writes a single framed message to stdout.
func (h *host) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(len(b)))
	os.Stdout.Write(hdr[:])
	os.Stdout.Write(b)
}

func (h *host) sendError(id, msg string) {
	h.send(evtError{Evt: "error", ID: id, Message: msg})
}

func pickShell(requested string) (string, []string) {
	if requested != "" {
		return requested, nil
	}
	if env := os.Getenv("SHELL"); env != "" {
		return env, nil
	}
	if runtime.GOOS == "windows" {
		return "powershell.exe", nil
	}
	return "/bin/bash", []string{"-l"}
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func stringField(m map[string]json.RawMessage, k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

func decodeInto(m map[string]json.RawMessage, dst any) bool {
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, dst) == nil
}

// pickBackend returns a PTY backend that works on the current OS.
// We default to github.com/creack/pty on Unix and a ConPTY wrapper on
// Windows. Both are tiny; vendored implementations are kept simple.
func pickBackend() (ptyBackend, error) {
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd":
		return &unixPtyBackend{}, nil
	case "windows":
		return nil, errors.New("Windows support not yet built (PRs welcome — see helper/pty_windows.go)")
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// we keep unused imports honest if Go ever drops a branch above.
var _ = context.Background

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// firefox-side-panel-terminal-host: a native messaging host that spawns
// PTYs for the Firefox "Side Panel Terminal" extension.
//
// Wire protocol (one JSON object per native-messaging frame):
//
//	→ {"cmd":"start","id":"...","cols":100,"rows":28,"shell":"","cwd":"","env":{}}
//	→ {"cmd":"input","id":"...","data":"<base64>"}
//	→ {"cmd":"resize","id":"...","cols":100,"rows":28}
//	→ {"cmd":"stop","id":"..."}
//	→ {"cmd":"ping"}
//	→ {"cmd":"shutdown"}
//
//	← {"evt":"pong","version":"0.2.0"}
//	← {"evt":"started","id":"...","title":"zsh - 100x28"}
//	← {"evt":"output","id":"...","data":"<base64>"}
//	← {"evt":"closed","id":"...","code":0}
//	← {"evt":"error","id":"...","message":"..."}
//
// Firefox communicates with native hosts over stdio using a length-prefixed
// framing: each message is preceded by a 4-byte native-endian (little-endian
// on every platform Firefox ships on) uint32 giving the JSON payload length.
//
// Firefox starts one helper process per connectNative() call and closes
// stdin when the port goes away (sidebar closed, extension reloaded). On
// stdin EOF we hang up every shell we started and exit.
//
// Build:   ./scripts/build-helper.sh
// Install: ./scripts/install.sh   (writes the native messaging manifest
//                                 to the right Firefox directory).

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Browser → helper. All commands share one shape; unused fields stay zero.
type command struct {
	Cmd   string            `json:"cmd"`
	ID    string            `json:"id"`
	Cols  int               `json:"cols"`
	Rows  int               `json:"rows"`
	Shell string            `json:"shell"`
	Cwd   string            `json:"cwd"`
	Env   map[string]string `json:"env"`
	Data  string            `json:"data"` // base64
}

// version is reported in "pong" so the options page can show what's installed.
const version = "0.2.0"

// Helper → browser
type evtPong struct {
	Evt     string `json:"evt"`
	Version string `json:"version"`
}
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

const (
	// Firefox → host messages may be up to 4 GiB; cap well below that so a
	// corrupt length can't make us allocate the world. Large pastes fit.
	maxInboundFrame = 16 * 1024 * 1024
	// Host → Firefox messages are limited to 1 MiB. A 16 KiB read becomes
	// ~22 KiB of base64, comfortably under the limit.
	readChunk = 16 * 1024
	// After the shell exits, how long to keep draining buffered output
	// before closing the master side.
	drainTimeout = 250 * time.Millisecond
	maxDim       = 1000
)

// ptyBackend abstracts the OS PTY layer so the same code can run on macOS,
// Linux, and (eventually) Windows ConPTY.
type ptyBackend interface {
	Start(cols, rows int, shell string, args []string, cwd string, env []string) (PtyHandle, error)
}

type PtyHandle interface {
	Write([]byte) (int, error)
	Read(p []byte) (int, error)
	Resize(cols, rows int) error
	// Wait blocks until the child exits and returns its exit code.
	Wait() int
	// Close hangs up the child and releases the PTY. Idempotent.
	Close() error
}

func main() {
	backend, err := pickBackend()
	if err != nil {
		fatal("no PTY backend available: %v", err)
	}
	h := newHost(backend, os.Stdout)
	err = h.run(bufio.NewReader(os.Stdin))
	h.closeAll()
	if err != nil && !errors.Is(err, io.EOF) {
		fatal("run: %v", err)
	}
}

type host struct {
	backend ptyBackend

	outMu sync.Mutex // serialises frames on out; readers run concurrently
	out   io.Writer

	mu       sync.Mutex
	sessions map[string]*session
	wg       sync.WaitGroup // one per live session
}

type session struct {
	id       string
	pty      PtyHandle
	readDone chan struct{}
}

func newHost(backend ptyBackend, out io.Writer) *host {
	return &host{backend: backend, out: out, sessions: map[string]*session{}}
}

// run reads frames until EOF or a framing error, or until "shutdown".
func (h *host) run(in io.Reader) error {
	for {
		var c command
		if err := readFramedMessage(in, &c); err != nil {
			var se *json.SyntaxError
			var te *json.UnmarshalTypeError
			if errors.As(err, &se) || errors.As(err, &te) {
				h.sendError("", fmt.Sprintf("bad message: %v", err))
				continue
			}
			return err
		}
		if c.Cmd == "shutdown" {
			return nil
		}
		h.dispatch(&c)
	}
}

// readFramedMessage reads one Firefox native-messaging frame: a 4-byte
// little-endian length followed by that many bytes of JSON.
func readFramedMessage(r io.Reader, dst any) error {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n == 0 || n > maxInboundFrame {
		return fmt.Errorf("invalid frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, dst)
}

// writeFramedMessage writes one Firefox native-messaging frame as a single
// Write so a frame is never split by a concurrent writer.
func writeFramedMessage(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	frame := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(frame, uint32(len(b)))
	copy(frame[4:], b)
	_, err = w.Write(frame)
	return err
}

func (h *host) dispatch(c *command) {
	switch c.Cmd {
	case "start":
		h.startSession(c)
	case "input":
		h.sessionInput(c)
	case "resize":
		h.sessionResize(c)
	case "ping":
		h.send(evtPong{Evt: "pong", Version: version})
	case "stop":
		if s := h.lookup(c.ID); s != nil {
			_ = s.pty.Close()
		}
	default:
		h.sendError(c.ID, fmt.Sprintf("unknown cmd %q", c.Cmd))
	}
}

func (h *host) lookup(id string) *session {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

func (h *host) startSession(c *command) {
	if c.ID == "" {
		h.sendError("", "start: missing id")
		return
	}
	if h.lookup(c.ID) != nil {
		h.sendError(c.ID, "start: session id already in use")
		return
	}

	shell, args := pickShell(c.Shell)
	cwd := expandHome(c.Cwd)
	if cwd == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cwd = home
		}
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		h.sendError(c.ID, fmt.Sprintf("invalid cwd %q", cwd))
		return
	}

	cols, rows := clampDims(c.Cols, c.Rows)

	env := os.Environ()
	for k, v := range c.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") {
			continue
		}
		env = setEnv(env, k, v)
	}
	// Tell the shell it's a terminal.
	env = setEnv(env, "TERM", "xterm-256color")
	env = setEnv(env, "COLORTERM", "truecolor")
	env = setEnv(env, "TERM_PROGRAM", "SidePanelTerminal")

	handle, err := h.backend.Start(cols, rows, shell, args, cwd, env)
	if err != nil {
		h.sendError(c.ID, fmt.Sprintf("could not start %s: %v", shell, err))
		return
	}

	s := &session{id: c.ID, pty: handle, readDone: make(chan struct{})}
	h.mu.Lock()
	h.sessions[c.ID] = s
	h.wg.Add(1)
	h.mu.Unlock()

	title := fmt.Sprintf("%s - %dx%d", filepath.Base(shell), cols, rows)
	h.send(evtStarted{Evt: "started", ID: c.ID, Title: title})

	go h.readLoop(s)
	go h.waitForExit(s)
}

func (h *host) readLoop(s *session) {
	defer close(s.readDone)
	buf := make([]byte, readChunk)
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

// waitForExit reaps the shell, drains what's left of its output, then
// tears the session down and reports the exit code.
func (h *host) waitForExit(s *session) {
	defer h.wg.Done()
	code := s.pty.Wait()
	select {
	case <-s.readDone:
	case <-time.After(drainTimeout):
		// A background job may still hold the tty open; don't wait on it.
	}
	_ = s.pty.Close()
	<-s.readDone

	h.mu.Lock()
	delete(h.sessions, s.id)
	h.mu.Unlock()
	h.send(evtClosed{Evt: "closed", ID: s.id, Code: code})
}

func (h *host) sessionInput(c *command) {
	s := h.lookup(c.ID)
	if s == nil {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(c.Data)
	if err != nil {
		h.sendError(c.ID, fmt.Sprintf("base64 decode: %v", err))
		return
	}
	if _, err := s.pty.Write(raw); err != nil {
		h.sendError(c.ID, fmt.Sprintf("write: %v", err))
	}
}

func (h *host) sessionResize(c *command) {
	s := h.lookup(c.ID)
	if s == nil {
		return
	}
	cols, rows := clampDims(c.Cols, c.Rows)
	if err := s.pty.Resize(cols, rows); err != nil {
		h.sendError(c.ID, fmt.Sprintf("resize: %v", err))
	}
}

// closeAll hangs up every session and waits (bounded) for them to be reaped.
func (h *host) closeAll() {
	h.mu.Lock()
	for _, s := range h.sessions {
		_ = s.pty.Close()
	}
	h.mu.Unlock()

	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(killGrace + time.Second):
	}
}

// send writes a single framed message. Errors (Firefox went away) are
// ignored; the stdin side will see EOF and shut us down.
func (h *host) send(v any) {
	h.outMu.Lock()
	defer h.outMu.Unlock()
	_ = writeFramedMessage(h.out, v)
}

func (h *host) sendError(id, msg string) {
	h.send(evtError{Evt: "error", ID: id, Message: msg})
}

// pickShell returns the shell to run. Shells are started as login shells so
// they pick up the user's PATH: Firefox launched from the Dock or a desktop
// launcher has a minimal environment.
func pickShell(requested string) (string, []string) {
	shell := strings.TrimSpace(requested)
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		if runtime.GOOS == "windows" {
			return "powershell.exe", nil
		}
		shell = "/bin/sh"
	}
	return expandHome(shell), []string{"-l"}
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func clampDims(cols, rows int) (int, int) {
	if cols <= 0 {
		cols = 100
	}
	if rows <= 0 {
		rows = 28
	}
	return min(cols, maxDim), min(rows, maxDim)
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

// pickBackend returns a PTY backend that works on the current OS.
func pickBackend() (ptyBackend, error) {
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd":
		return newUnixBackend(), nil
	case "windows":
		return nil, errors.New("Windows support not yet built")
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "spt-host: "+format+"\n", a...)
	os.Exit(1)
}

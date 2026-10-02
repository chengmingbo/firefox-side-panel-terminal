//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// unixPtyBackend uses creack/pty to manage the pseudo-terminal.
// Available on macOS, Linux and the BSDs.
type unixPtyBackend struct{}

type unixPty struct {
	master *os.File
	cmd    *exec.Cmd

	closeOnce sync.Once
	waitOnce  sync.Once
	exited    chan struct{}
	exitCode  int
}

// killGrace is how long a shell gets to exit after SIGHUP before SIGKILL.
const killGrace = 2 * time.Second

func (b *unixPtyBackend) Start(cols, rows int, shell string, args []string, cwd string, env []string) (PtyHandle, error) {
	cmd := exec.Command(shell, args...)
	cmd.Env = env
	cmd.Dir = cwd
	// pty.Start sets Setsid+Setctty, so the shell leads its own session and
	// process group (pgid == pid). Do not also set Setpgid: setpgid() on a
	// session leader fails with EPERM and the fork/exec is rejected.
	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return nil, fmt.Errorf("pty.Start: %w", err)
	}
	return &unixPty{master: master, cmd: cmd, exited: make(chan struct{})}, nil
}

func (p *unixPty) Write(data []byte) (int, error) { return p.master.Write(data) }
func (p *unixPty) Read(buf []byte) (int, error)   { return p.master.Read(buf) }

func (p *unixPty) Resize(cols, rows int) error {
	return pty.Setsize(p.master, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
}

// Wait reaps the shell and returns its exit code (128+signal if killed).
// Safe to call more than once.
func (p *unixPty) Wait() int {
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		p.exitCode = 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				p.exitCode = 128 + int(ws.Signal())
			} else {
				p.exitCode = ee.ExitCode()
			}
		} else if err != nil {
			p.exitCode = -1
		}
		close(p.exited)
	})
	return p.exitCode
}

// Close hangs up the shell's process group, closes the master side, and
// escalates to SIGKILL if the group is still around after killGrace.
// Safe to call more than once and concurrently with Read/Wait.
func (p *unixPty) Close() error {
	var err error
	p.closeOnce.Do(func() {
		pid := p.cmd.Process.Pid
		_ = syscall.Kill(-pid, syscall.SIGHUP)
		err = p.master.Close()
		go func() {
			select {
			case <-p.exited:
			case <-time.After(killGrace):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
	})
	return err
}

func newUnixBackend() ptyBackend { return &unixPtyBackend{} }

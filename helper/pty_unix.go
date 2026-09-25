//go:build unix || darwin || linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// unixPtyBackend uses creack/pty to manage the pseudo-terminal.
// Available on macOS, Linux and the BSDs.
type unixPtyBackend struct{}

type unixPty struct {
	master *os.File
	cmd    *exec.Cmd
	closed bool
}

func (b *unixPtyBackend) Start(cols, rows int, shell string, args []string, cwd string, env []string) (PtyHandle, error) {
	cmd := exec.Command(shell, args...)
	cmd.Env = env
	cmd.Dir = cwd
	// Put the child in its own process group so a Ctrl-C in the terminal
	// only kills the shell (and its children) — not the helper.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	master, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return nil, fmt.Errorf("pty.Start: %w", err)
	}
	return &unixPty{master: master, cmd: cmd}, nil
}

func (p *unixPty) Write(data []byte) (int, error) {
	if p.closed {
		return 0, errors.New("pty closed")
	}
	return p.master.Write(data)
}

func (p *unixPty) Read(buf []byte) (int, error) {
	if p.closed {
		return 0, errors.New("pty closed")
	}
	return p.master.Read(buf)
}

func (p *unixPty) Resize(cols, rows int) error {
	if p.closed {
		return nil
	}
	return pty.Setsize(p.master, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
}

func (p *unixPty) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	// Best-effort: kill the whole process group so any children go too.
	if p.cmd != nil && p.cmd.Process != nil {
		pgid, err := syscall.Getpgid(p.cmd.Process.Pid)
		if err == nil {
			_ = syscall.Kill(-pgid, syscall.SIGHUP)
		}
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	return p.master.Close()
}

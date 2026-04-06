package pty

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// PTY wraps a pseudo-terminal master file descriptor and the subprocess running inside it.
type PTY struct {
	ptmx *os.File
	cmd  *exec.Cmd
	done chan struct{}
	err  error

	closeOnce sync.Once
}

// Start spawns the given command inside a new PTY sized to rows x cols.
func (p *PTY) Start(cmd *exec.Cmd, rows, cols uint16) error {
	size := &pty.Winsize{Rows: rows, Cols: cols}
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return err
	}

	p.ptmx = ptmx
	p.cmd = cmd
	p.done = make(chan struct{})

	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()

	return nil
}

// Read reads raw bytes from the PTY master fd.
func (p *PTY) Read(buf []byte) (int, error) {
	return p.ptmx.Read(buf)
}

// Write sends bytes to the subprocess stdin via the PTY master fd.
func (p *PTY) Write(data []byte) (int, error) {
	return p.ptmx.Write(data)
}

// Resize changes the PTY window size, sending SIGWINCH to the subprocess.
func (p *PTY) Resize(rows, cols uint16) error {
	return pty.Setsize(p.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Close sends SIGTERM to the subprocess, waits briefly, then closes the PTY fd.
func (p *PTY) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			// Send SIGTERM.
			_ = p.cmd.Process.Signal(syscall.SIGTERM)

			// Wait up to 3 seconds for the process to exit.
			select {
			case <-p.done:
			case <-time.After(3 * time.Second):
				// Force kill if it hasn't exited.
				_ = p.cmd.Process.Kill()
				<-p.done
			}
		}

		if p.ptmx != nil {
			closeErr = p.ptmx.Close()
		}
	})
	return closeErr
}

// Pid returns the process ID of the spawned subprocess.
// Returns 0 if the process has not been started yet.
func (p *PTY) Pid() int {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Pid
	}
	return 0
}

// Done returns a channel that fires when the subprocess exits.
func (p *PTY) Done() <-chan struct{} {
	return p.done
}

// Err returns the subprocess exit error after Done fires.
// Returns nil if the process exited successfully.
func (p *PTY) Err() error {
	select {
	case <-p.done:
		return p.err
	default:
		return errors.New("process still running")
	}
}

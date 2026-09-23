package credentialsinit

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/charmbracelet/x/term"
)

func isTerminal(f *os.File) bool { return term.IsTerminal(f.Fd()) }

// terminal is the prompter at a real tty: prompts to out (stderr), answers
// from in (stdin).
type terminal struct {
	in  *os.File
	out io.Writer
}

func newTerminal(in *os.File, out io.Writer) terminal { return terminal{in: in, out: out} }

func (t terminal) Line(prompt string) (string, error) {
	_, _ = fmt.Fprint(t.out, prompt)
	return readLine(t.in)
}

// readLine reads one line a byte at a time. Not through a bufio.Reader: that
// would swallow typed-ahead input, which ReadPassword -- reading the file
// descriptor directly -- would then never see. End of input with nothing typed
// is io.EOF; a partial line before it is returned as the answer.
func readLine(r io.Reader) (string, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n == 1 {
			if b[0] == '\n' {
				return string(line), nil
			}
			line = append(line, b[0])
			continue
		}
		if err == io.EOF && len(line) > 0 {
			return string(line), nil
		}
		if err != nil {
			return "", err
		}
	}
}

// Secret reads a line with echo off.
//
// ReadPassword restores the terminal in a defer, but it leaves ISIG set, so a
// Ctrl-C kills the process before the defer runs and leaves the shell with
// echo off. The state is saved first and restored on the signals that end a
// process at a terminal.
func (t terminal) Secret(prompt string) (string, error) {
	_, _ = fmt.Fprint(t.out, prompt)
	fd := t.in.Fd()
	state, err := term.GetState(fd)
	if err != nil {
		return "", err
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			_ = term.Restore(fd, state)
			_, _ = fmt.Fprintln(t.out)
			os.Exit(130)
		case <-done:
		}
	}()
	b, err := term.ReadPassword(fd)
	close(done)
	signal.Stop(sigs)
	// With echo off, the Enter that ended the line did not move the cursor.
	_, _ = fmt.Fprintln(t.out)
	return string(b), err
}

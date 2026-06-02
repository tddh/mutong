package iostreams

import (
	"bytes"
	"io"
	"os"

	"golang.org/x/term"
)

type IOStreams struct {
	In           io.Reader
	Out          io.Writer
	ErrOut       io.Writer
	isTTY        bool
	colorEnabled bool
}

func System() *IOStreams {
	tty := isTerminal(os.Stdout)
	return &IOStreams{
		In:           os.Stdin,
		Out:          os.Stdout,
		ErrOut:       os.Stderr,
		isTTY:        tty,
		colorEnabled: tty && os.Getenv("NO_COLOR") == "",
	}
}

func Test() (*IOStreams, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &IOStreams{
		In:     &bytes.Buffer{},
		Out:    stdout,
		ErrOut: stderr,
	}, stdout, stderr
}

func (s *IOStreams) IsStdoutTTY() bool { return s.isTTY }

func (s *IOStreams) ColorEnabled() bool { return s.colorEnabled }

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

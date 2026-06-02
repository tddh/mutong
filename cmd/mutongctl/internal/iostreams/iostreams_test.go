package iostreams

import (
	"testing"
)

func TestSystem(t *testing.T) {
	ios := System()
	if ios.In == nil {
		t.Error("System() In should not be nil")
	}
	if ios.Out == nil {
		t.Error("System() Out should not be nil")
	}
	if ios.ErrOut == nil {
		t.Error("System() ErrOut should not be nil")
	}
}

func TestTest(t *testing.T) {
	ios, stdout, stderr := Test()
	if ios.In == nil {
		t.Error("Test() In should not be nil")
	}
	if stdout == nil {
		t.Error("Test() should return non-nil stdout buffer")
	}
	if stderr == nil {
		t.Error("Test() should return non-nil stderr buffer")
	}
	ios.Out.Write([]byte("hello"))
	if stdout.String() != "hello" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello")
	}
}

func TestIsStdoutTTY_NonTTY(t *testing.T) {
	ios, _, _ := Test()
	if ios.IsStdoutTTY() {
		t.Error("Test() IOStreams should NOT report TTY")
	}
}

func TestColorEnabled_NonTTY(t *testing.T) {
	ios, _, _ := Test()
	if ios.ColorEnabled() {
		t.Error("Test() IOStreams should NOT have color enabled")
	}
}

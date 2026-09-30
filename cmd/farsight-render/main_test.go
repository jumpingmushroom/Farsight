package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunRejectsInvalidGenVersion checks that run() reports an out-of-range
// -gen as a usageError, distinct from a runtime failure such as a render
// error. main maps a usageError to exit code 2; that mapping is checked at
// the process level by TestMainExitsTwoOnInvalidGen below, since os.Exit
// can't be observed by calling main() in-process.
func TestRunRejectsInvalidGenVersion(t *testing.T) {
	for _, gen := range []int32{-1, 3, 99} {
		err := run(context.Background(), "FjordSeed", gen, t.TempDir(), "")
		var ue usageError
		if !errors.As(err, &ue) {
			t.Errorf("run with gen=%d: err=%v, want a usageError", gen, err)
		}
	}
}

// TestMainExitsTwoOnInvalidGen builds the binary and runs it with an
// out-of-range -gen, checking the actual process exit code and that the
// error message names the bad value.
func TestMainExitsTwoOnInvalidGen(t *testing.T) {
	bin := buildFarsightRender(t)
	cmd := exec.Command(bin, "-seed=FjordSeed", "-gen=99", "-out="+t.TempDir())
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run: err=%v (want an *exec.ExitError); output: %s", err, out)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2; output: %s", exitErr.ExitCode(), out)
	}
	if !strings.Contains(string(out), "genVersion") || !strings.Contains(string(out), "99") {
		t.Fatalf("output %q does not clearly name the invalid genVersion", out)
	}
}

func buildFarsightRender(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "farsight-render")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

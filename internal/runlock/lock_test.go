package runlock

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockProcess(t *testing.T) {
	if os.Getenv("NARU_LOCK_HELPER") != "1" {
		return
	}
	lock, err := Acquire(os.Getenv("NARU_LOCK_ROOT"), "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadByte(); err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestIndependentProcessesAndAliases(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestLockProcess$") // #nosec G204 G702 -- Re-executes this test binary with a fixed helper test.
	cmd.Env = append(os.Environ(), "NARU_LOCK_HELPER=1", "NARU_LOCK_ROOT="+root)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := false
	t.Cleanup(func() {
		if !done {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = in.Close()
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("helper: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not acquire lock")
	}
	for _, check := range []string{"other"} {
		l, err := Acquire(root, check)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	l, err := Acquire(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err == nil {
		if l, err := Acquire(alias, "test"); !errors.Is(err, ErrBusy) {
			if l != nil {
				_ = l.Close()
			}
			t.Fatalf("alias bypass: %v", err)
		}
	}
	if l, err := Acquire(root, "test"); !errors.Is(err, ErrBusy) {
		if l != nil {
			_ = l.Close()
		}
		t.Fatalf("duplicate acquired: %v", err)
	}
	if _, err := in.Write([]byte{'x'}); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	done = true
	l, err = Acquire(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/state/locks/test.lock")); err != nil {
		t.Fatal("lock file was removed", err)
	}
}

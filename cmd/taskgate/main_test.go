package main

import (
	"bytes"
	"testing"
)

func TestRejectInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{nil, {"check", "--unknown"}, {"check", "--task", "NARU-001", "--next", "NARU-002"}, {"check", "extra"}} {
		var out, errout bytes.Buffer
		if code := run(args, &out, &errout); code != 64 {
			t.Fatalf("%v: %d %s", args, code, out.String())
		}
	}
}

package runner

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestSurfaceCaptureLimit(t *testing.T) {
	cancelled := false
	out := limitedOutput{limit: 8, cancel: func() { cancelled = true }}
	// Pipe readers use io.Copy's ReaderFrom optimization when the destination
	// exposes it. The size guard must hold on that path too.
	_, err := io.Copy(&out, io.LimitReader(strings.NewReader(strings.Repeat("x", 32)), 32))
	if !errors.Is(err, ErrSurfaceOutput) || !cancelled || len(out.Bytes()) > 8 {
		t.Fatalf("capture limit bypassed: bytes=%d cancelled=%v err=%v", len(out.Bytes()), cancelled, err)
	}
}

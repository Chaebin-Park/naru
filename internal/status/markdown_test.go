package status

import (
	"errors"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"strings"
	"testing"
)

type markdownFailedWriter struct{}

func (markdownFailedWriter) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestMarkdownReport(t *testing.T) {
	rep := Report{Mode: "code", Satisfied: true, Unverified: UnverifiedState{Present: true}, Checks: []CheckStatus{{Check: "x|<script>\n[link](target)", State: Pass, Reason: "relay_verified", Provenance: &receipt.Provenance{Kind: receipt.EvidenceHumanRelayed}}}}
	var out strings.Builder
	if err := WriteMarkdown(&out, rep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**satisfied**", "NOT VERIFIED", "human-relayed", "x&#124;&lt;script&gt; &#91;link&#93;(target)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, out.String())
		}
	}
	if err := WriteMarkdown(markdownFailedWriter{}, rep); err == nil {
		t.Fatal("output error ignored")
	}
}

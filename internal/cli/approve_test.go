package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Chaebin-Park/naru/internal/baseline"
)

func TestApprovalRequiresTTY(t *testing.T) {
	root := newProject(t, false)
	write(t, root, ".naru/config.yaml", projectConfig+"surfaces: {api: {run: 'true', inputs: [input.txt], extractor_version: v1}}\n")
	if got := run(t, root, "Reviewer\nReason\napprove anything\n", "baseline", "approve", "api"); got.code != 64 || !strings.Contains(got.stderr, "requires terminal") {
		t.Fatalf("nonterminal approval %+v", got)
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := null.Close(); err != nil {
			t.Error(err)
		}
	}()
	if isTerminal(null) {
		t.Fatal("character device mistaken for terminal")
	}
	if _, err := os.Stat(filepath.Join(root, ".naru/baselines")); !os.IsNotExist(err) {
		t.Fatal("nonterminal approval mutated baseline")
	}
}
func TestApprovalReviewFlow(t *testing.T) {
	root := newProject(t, false)
	write(t, root, ".naru/config.yaml", projectConfig+"surfaces: {api: {run: 'cat output.json', inputs: [output.json], extractor_version: v1}}\n")
	write(t, root, "output.json", `{"schema":1,"items":[]}`)
	if got := run(t, root, "", "baseline", "extract", "api"); got.code != 0 {
		t.Fatal(got)
	}
	preview, err := baseline.Diff(root, "api")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"Reviewer\nReason\nwrong\n", "Reviewer\nReason\napprove " + preview.NewDigest + "\n", strings.Repeat("x", 3000) + "\nReason\n", ""} {
		var output bytes.Buffer
		if _, err := reviewBaseline(context.Background(), root, "api", Env{Stdin: strings.NewReader(input), Stderr: &output}); err == nil {
			t.Fatal("invalid confirmation accepted")
		}
	}
	var output bytes.Buffer
	record, err := reviewBaseline(context.Background(), root, "api", Env{Stdin: strings.NewReader("Reviewer\nEmpty reviewed\napprove empty " + preview.NewDigest + "\n"), Stderr: &output})
	if err != nil {
		t.Fatal(err)
	}
	if record.Approval.Reviewer != "Reviewer" || !strings.Contains(output.String(), `"candidate_count": 0`) {
		t.Fatal("review evidence missing")
	}
	data, err := os.ReadFile(filepath.Join(root, ".naru/baselines/api.json")) // #nosec G304 -- Test-owned temporary root and fixed approval fixture.
	if err != nil || !json.Valid(data) {
		t.Fatalf("publication %s %v", data, err)
	}
	if got := run(t, root, "", "baseline", "diff", "api"); got.code != 0 {
		t.Fatal(got)
	}
}

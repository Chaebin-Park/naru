package cli

import (
	"encoding/json"
	"github.com/Chaebin-Park/naru/internal/receipt"
	"github.com/Chaebin-Park/naru/internal/status"
	"testing"
)

func TestCIExecutionFreshness(t *testing.T) {
	root := newProject(t, false)
	write(t, root, ".naru/config.yaml", "version: 1\ndefault_mode: ci\nmodes: {ci: [c]}\nchecks: {c: {run: 'test \"$NARU_TEST_CI_FAIL\" != 1', inputs: [src/**]}}\n")
	t.Setenv("NARU_TEST_CI_FAIL", "")
	if r := run(t, root, "", "check", "--evidence", "local"); r.code != 0 {
		t.Fatal(r)
	}
	// Environment is deliberately outside the input digest. A matching local
	// pass still cannot substitute for executing the CI command.
	t.Setenv("NARU_TEST_CI_FAIL", "1")
	if r := run(t, root, "", "check", "--evidence", "ci"); r.code != 1 {
		t.Fatalf("local pass reused: %+v", r)
	}
	r := run(t, root, "", "status", "--json")
	var rep status.Report
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Satisfied || rep.Checks[0].State != status.Fail || rep.Checks[0].Provenance.Kind != receipt.EvidenceCI {
		t.Fatalf("%+v", rep)
	}
	t.Setenv("NARU_TEST_CI_FAIL", "")
	if r := run(t, root, "", "check", "--evidence", "ci"); r.code != 0 {
		t.Fatal(r)
	}
}

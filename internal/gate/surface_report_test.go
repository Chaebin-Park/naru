package gate

import (
	"github.com/Chaebin-Park/naru/internal/status"
	"testing"
)

func TestSurfaceReportFingerprint(t *testing.T) {
	rep := status.Report{Mode: "m", Surfaces: []status.SurfaceStatus{{Surface: "api", State: "changed", InputDigest: "input", CandidateDigest: "candidate", BaselineRevision: "approval"}}}
	before := reportFingerprint(rep)
	for _, field := range []*string{&rep.Surfaces[0].State, &rep.Surfaces[0].InputDigest, &rep.Surfaces[0].CandidateDigest, &rep.Surfaces[0].BaselineRevision} {
		old := *field
		*field = "different"
		if reportFingerprint(rep) == before {
			t.Fatal("surface progress ignored")
		}
		*field = old
	}
}

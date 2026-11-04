package cli

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Chaebin-Park/naru/internal/surface"
)

func springFixtures(t *testing.T, cases ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Spring pack requires POSIX flock; Windows runtime unsupported")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python 3 unavailable; Spring pack fixture runtime not tested")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := append([]string{"-B", "../../packs/spring-boot/test_extract.py"}, cases...)
	// #nosec G204 -- Fixed repository fixture test script; interpreter resolved from PATH like other prerequisites.
	output, err := exec.CommandContext(ctx, python, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("Spring fixture checks: %v\n%s", err, output)
	}
}
func TestSpringBootFixtures(t *testing.T) {
	springFixtures(t, "ContractFixtures.test_contract_changes", "ContractFixtures.test_resources")
	for _, fixture := range []string{"normal/contract-api.json", "normal/extra-api.json", "normal/contract-routes.json", "normal/extra-routes.json", "broken/contract-api.json", "broken/extra-api.json", "broken/contract-routes.json", "broken/extra-routes.json"} {
		data, err := os.ReadFile("testdata/spring-boot/" + fixture) // #nosec G304 -- Fixed repository fixture names; no external path input.
		if err != nil {
			t.Fatal(err)
		}
		if _, err := surface.Build(surface.Request{Schema: 1, SurfaceID: "spring-boot.api", ExtractorVersion: "spring-boot-v1", InputDigest: strings.Repeat("a", 64)}, data, nil); err != nil {
			t.Fatalf("fixture %s violates surface contract: %v", fixture, err)
		}
	}
}
func TestSpringBootInvalidOutput(t *testing.T) {
	springFixtures(t, "ContractFixtures.test_invalid_exports", "ContractFixtures.test_no_old_exports_or_parallel_build", "ContractFixtures.test_unsupported_configuration")
}

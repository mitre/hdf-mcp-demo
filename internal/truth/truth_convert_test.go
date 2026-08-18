package truth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestClassify_GrypeConvert pins the grype candidates against a document produced
// by the shipped `hdf` binary at test time. grype's HDF conversion is ~1 MB (too
// large to vendor as the gosec doc is), so this is gated on HDF_BIN (or `hdf` on
// PATH) and skips otherwise; the offline gosec pins remain the primary contract.
//
// It demonstrates the honest contrast to gosec: grype does not deduplicate, so
// its match count is answer-preserving (Class A, 89 == 89) where gosec's is
// divergent (Class B, 7 vs 3). Divergence is a property of the scanner's output
// shape, not of normalization in general.
func TestClassify_GrypeConvert(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the grype convert pins")
		}
		bin = p
	}

	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "grype.json"))
	if err != nil {
		t.Fatalf("read raw fixture: %v", err)
	}

	hdfPath := filepath.Join(t.TempDir(), "grype.hdf.json")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "convert",
		filepath.Join("..", "..", "fixtures", "grype.json"), "--from", "grype", "-o", hdfPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hdf convert grype: %v: %s", err, out)
	}
	hdf, err := os.ReadFile(hdfPath)
	if err != nil {
		t.Fatalf("read converted hdf: %v", err)
	}

	byID := map[string]Candidate{}
	for _, c := range Bank() {
		byID[c.ID] = c
	}

	cases := []struct {
		id        string
		wantClass Class
	}{
		{"grype-match-count", ClassA},     // 89 raw matches == 89 requirements (no dedup)
		{"grype-cve-present", ClassA},     // existence survives normalization
		{"grype-compliance-rate", ClassC}, // a vuln scan has no native pass rate
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			got, err := Classify(byID[tc.id].Question, [][]byte{raw}, [][]byte{hdf})
			if err != nil {
				t.Fatalf("classify: %v", err)
			}
			if got.Class != tc.wantClass {
				t.Errorf("class = %s, want %s (raw=%+v hdf=%+v)", got.Class, tc.wantClass, got.RawAnswer, got.HDFAnswer)
			}
		})
	}
}

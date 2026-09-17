package benchmark

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestMergedSample_MatchesPipelineOutput guards the committed pipeline sample
// (fixtures/merged-gosec-zap-grype.hdf.json, see fixtures/PROVENANCE.md): it is
// derived, so it must be byte-identical to what the shipped CLI produces from
// the raw fixtures today — three conversions and one merge. A converter or merge
// change that alters the document fails here rather than leaving a stale sample
// that no longer matches what the benchmark stages. Gated on HDF_BIN because it
// runs the real binary.
func TestMergedSample_MatchesPipelineOutput(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to regenerate and compare the merged sample")
		}
		bin = p
	}
	want, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "merged-gosec-zap-grype.hdf.json"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	ctx := context.Background()
	converted := make([]string, 0, 3)
	for _, f := range []string{"gosec", "zap", "grype"} {
		out := filepath.Join(dir, f+".hdf.json")
		cmd := exec.CommandContext(ctx, bin, "convert", "--from", f, filepath.Join("..", "..", "fixtures", f+".json"), "-o", out)
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hdf convert %s: %v: %s", f, err, o)
		}
		converted = append(converted, out)
	}
	merged := filepath.Join(dir, "merged.hdf.json")
	args := append([]string{"merge"}, converted...)
	args = append(args, "-o", merged)
	if o, err := exec.CommandContext(ctx, bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("hdf merge: %v: %s", err, o)
	}
	got, err := os.ReadFile(merged)
	if err != nil {
		t.Fatal(err)
	}
	// gosec output carries no timestamp, so its converter stamps the conversion
	// time into results[].startTime, which then becomes the merged root's latest
	// timestamp and the gosec provenance entry. Those are the only bytes that
	// legitimately differ between regenerations; everything else must match.
	if !bytes.Equal(maskTimestamps(got), maskTimestamps(want)) {
		t.Fatalf("fixtures/merged-gosec-zap-grype.hdf.json (%d bytes) no longer matches the pipeline output (%d bytes) apart from timestamps — regenerate it with the commands in fixtures/PROVENANCE.md and record the new sha256", len(want), len(got))
	}
	if bytes.Equal(got, want) {
		t.Logf("regeneration is byte-identical (the gosec conversion timestamp happened to match)")
	}
}

// rfc3339 matches the trimmed-UTC timestamps HDF documents carry.
var rfc3339 = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

func maskTimestamps(b []byte) []byte {
	return rfc3339.ReplaceAll(b, []byte("<timestamp>"))
}

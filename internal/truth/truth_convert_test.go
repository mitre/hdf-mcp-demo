package truth

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestClassify_CrossFormatConvert pins the cross-format aggregate's class against
// real conversions of all three sources: raw 60 (0+3+57 across three severity
// vocabularies) must equal the HDF view's impact>=0.7 sum — Class A. This holds
// only while every converter's severity→impact mapping stays band-aligned
// (gosec MEDIUM→0.5, zap riskcode3→0.7, grype High/Critical→0.7/0.9); a mapping
// change that breaks the alignment fails here loudly.
func TestClassify_CrossFormatConvert(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the cross-format pins")
		}
		bin = p
	}
	var rawDocs, hdfDocs [][]byte
	for _, f := range []struct{ raw, from string }{
		{"gosec.json", "gosec"}, {"zap.json", "zap"}, {"grype.json", "grype"},
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", f.raw))
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), f.raw+".hdf.json")
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		cmd := exec.CommandContext(ctx, bin, "convert", filepath.Join("..", "..", "fixtures", f.raw), "--from", f.from, "-o", out)
		combined, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("convert %s: %v: %s", f.raw, err, combined)
		}
		hdf, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		rawDocs, hdfDocs = append(rawDocs, raw), append(hdfDocs, hdf)
	}
	q := Question{
		ID:  "cross-format-high-count",
		Raw: CrossFormatHighSeverityCount, HDF: HDFImpactTotalAtLeast(0.7),
	}
	got, err := Classify(q, rawDocs, hdfDocs)
	if err != nil {
		t.Fatal(err)
	}
	if got.Class != ClassA || got.RawAnswer.Value != "60" || got.HDFAnswer.Value != "60" {
		t.Errorf("cross-format: class %s raw %s hdf %s; want A 60 60", got.Class, got.RawAnswer.Value, got.HDFAnswer.Value)
	}
}

// convertFixture converts one raw fixture with the shipped binary and returns
// both byte views. args is the full argv after the binary (with {raw}/{hdf}
// substituted), so the SBOM's `system create` path is expressible too.
func convertFixture(t *testing.T, bin, fixture string, args ...string) (raw, hdf []byte) {
	t.Helper()
	rawPath := filepath.Join("..", "..", "fixtures", fixture)
	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), fixture+".hdf.json")
	argv := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "{raw}", rawPath)
		argv[i] = strings.ReplaceAll(a, "{hdf}", out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if combined, err := exec.CommandContext(ctx, bin, argv...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", bin, argv, err, combined)
	}
	hdf, err = os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return raw, hdf
}

// TestClassify_TemporalDiffConvert pins the temporal-diff question end to end:
// the distinct-fixed count must survive conversion (requirement IDs are the
// vulnerability IDs, deduplicated per doc) — 5 == 5, Class A.
func TestClassify_TemporalDiffConvert(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the temporal-diff pins")
		}
		bin = p
	}
	rawA, hdfA := convertFixture(t, bin, "grype-alpine311.json", "convert", "{raw}", "--from", "grype", "-o", "{hdf}")
	rawB, hdfB := convertFixture(t, bin, "grype-alpine312.json", "convert", "{raw}", "--from", "grype", "-o", "{hdf}")
	q := Question{ID: "grype-fixed-vulns", Raw: GrypeDistinctFixedCount, HDF: HDFDistinctFixedCount}
	got, err := Classify(q, [][]byte{rawA, rawB}, [][]byte{hdfA, hdfB})
	if err != nil {
		t.Fatal(err)
	}
	if got.Class != ClassA || got.RawAnswer.Value != "5" || got.HDFAnswer.Value != "5" {
		t.Errorf("temporal diff: class %s raw %s hdf %s; want A 5 5", got.Class, got.RawAnswer.Value, got.HDFAnswer.Value)
	}
}

// TestClassify_JoinConvert pins the SBOM-join question as the bank's first
// GENUINE Class D (raw-only): `hdf system create --from spdx` carries the SBOM
// as a reference (boms[].ref) with no embedded package inventory, so the HDF
// document simply does not contain the join key — the HDF view is unanswerable
// and the raw view (15 SBOM packages − 7 with matches = 8) is the only one.
// If a future converter embeds the BOM document, HDFVulnFreePackageCount starts
// answering and this question auto-reclassifies — which is the ADR-0002 design,
// and this pin fails loudly so the bank note gets updated too.
func TestClassify_JoinConvert(t *testing.T) {
	bin := os.Getenv("HDF_BIN")
	if bin == "" {
		p, err := exec.LookPath("hdf")
		if err != nil {
			t.Skip("set HDF_BIN=/path/to/hdf (or put hdf on PATH) to run the join pins")
		}
		bin = p
	}
	rawG, hdfG := convertFixture(t, bin, "grype-alpine312.json", "convert", "{raw}", "--from", "grype", "-o", "{hdf}")
	rawS, hdfS := convertFixture(t, bin, "spdx-alpine312.json", "system", "create", "{raw}", "--from", "spdx", "-o", "{hdf}")
	q := Question{ID: "sbom-vuln-free-packages", Raw: SBOMVulnFreePackageCount, HDF: HDFVulnFreePackageCount}
	got, err := Classify(q, [][]byte{rawG, rawS}, [][]byte{hdfG, hdfS})
	if err != nil {
		t.Fatal(err)
	}
	if got.Class != ClassD || got.RawAnswer.Value != "8" || got.HDFAnswer.Answerable {
		t.Errorf("join: class %s raw %s hdf %+v; want D 8 unanswerable", got.Class, got.RawAnswer.Value, got.HDFAnswer)
	}
}

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

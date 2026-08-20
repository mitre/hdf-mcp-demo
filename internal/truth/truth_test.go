package truth

import (
	"os"
	"path/filepath"
	"testing"
)

// classOf is the whole taxonomy in one function; exercise every arm directly.
func TestClassOf(t *testing.T) {
	cases := []struct {
		name     string
		raw, hdf Answer
		want     Class
	}{
		{"agree -> A", Answered("7"), Answered("7"), ClassA},
		{"disagree -> B", Answered("7"), Answered("3"), ClassB},
		{"raw cannot answer -> C", Unanswerable(), Answered("40"), ClassC},
		{"both cannot answer -> B", Unanswerable(), Unanswerable(), ClassB},
		{"hdf cannot answer -> D", Answered("7"), Unanswerable(), ClassD},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classOf(c.raw, c.hdf); got != c.want {
				t.Errorf("classOf(%+v,%+v) = %s, want %s", c.raw, c.hdf, got, c.want)
			}
		})
	}
}

// The raw-view parsers, on small real-shaped inputs.
func TestRawParsers(t *testing.T) {
	gosec := []byte(`{"Issues":[{"rule_id":"G304","severity":"MEDIUM"},{"rule_id":"G304","severity":"MEDIUM"},{"rule_id":"G401","severity":"HIGH"}]}`)
	if a, err := GosecFindingCount(gosec); err != nil || a.Value != "3" {
		t.Errorf("GosecFindingCount = %+v, err %v; want 3", a, err)
	}
	if a, _ := GosecRulePresent("G304")(gosec); a.Value != "true" {
		t.Errorf("GosecRulePresent(G304) = %+v; want true", a)
	}
	if a, _ := GosecRulePresent("G999")(gosec); a.Value != "false" {
		t.Errorf("GosecRulePresent(G999) = %+v; want false", a)
	}
	if a, _ := AlwaysUnanswerable([][]byte{gosec}); a.Answerable {
		t.Errorf("gosec compliance rate should be unanswerable, got %+v", a)
	}

	grype := []byte(`{"matches":[{"vulnerability":{"id":"CVE-2021-36159","severity":"Critical"}},{"vulnerability":{"id":"CVE-2020-0001","severity":"Low"}}]}`)
	if a, err := GrypeMatchCount(grype); err != nil || a.Value != "2" {
		t.Errorf("GrypeMatchCount = %+v, err %v; want 2", a, err)
	}
	if a, _ := GrypeCVEPresent("CVE-2021-36159")(grype); a.Value != "true" {
		t.Errorf("GrypeCVEPresent(present) = %+v; want true", a)
	}
	if a, _ := GrypeCVEPresent("CVE-9999-0000")(grype); a.Value != "false" {
		t.Errorf("GrypeCVEPresent(absent) = %+v; want false", a)
	}
	if a, _ := GrypeSeverityPresent("Critical")(grype); a.Value != "true" {
		t.Errorf("GrypeSeverityPresent(Critical) = %+v; want true", a)
	}
	if a, _ := GrypeSeverityPresent("Nope")(grype); a.Value != "false" {
		t.Errorf("GrypeSeverityPresent(absent) = %+v; want false", a)
	}

	zap := []byte(`{"site":[{"alerts":[{"riskcode":"3"},{"riskcode":"1"}]},{"alerts":[{"riskcode":"3"},{"riskcode":"0"}]}]}`)
	if a, err := ZapAlertCount(zap); err != nil || a.Value != "4" {
		t.Errorf("ZapAlertCount = %+v, err %v; want 4", a, err)
	}
	if a, _ := ZapHighCount(zap); a.Value != "2" {
		t.Errorf("ZapHighCount = %+v; want 2", a)
	}
}

// The HDF-view parsers, on a small hand-built document (two rules, one deduped
// requirement carrying two results, a passed and a failed).
func TestHDFParsers(t *testing.T) {
	doc := []byte(`{"baselines":[{"requirements":[
		{"id":"Grype/CVE-2021-36159","impact":0.9,"results":[{"status":"failed"},{"status":"passed"}]},
		{"id":"G304","impact":0.5,"results":[{"status":"failed"}]}
	]}]}`)
	if a, err := HDFRequirementCount(doc); err != nil || a.Value != "2" {
		t.Errorf("HDFRequirementCount = %+v, err %v; want 2", a, err)
	}
	if a, _ := HDFRequirementPresent("G304")(doc); a.Value != "true" {
		t.Errorf("HDFRequirementPresent(exact) = %+v; want true", a)
	}
	if a, _ := HDFRequirementPresent("CVE-2021-36159")(doc); a.Value != "true" {
		t.Errorf("HDFRequirementPresent(namespaced suffix) = %+v; want true", a)
	}
	if a, _ := HDFRequirementPresent("CVE-0000-0000")(doc); a.Value != "false" {
		t.Errorf("HDFRequirementPresent(absent) = %+v; want false", a)
	}
	// 1 passed of 3 total results -> 33%.
	if a, err := HDFComplianceRate(doc); err != nil || a.Value != "33" {
		t.Errorf("HDFComplianceRate = %+v, err %v; want 33", a, err)
	}
	// impacts in doc: 0.9 and 0.5.
	if a, _ := HDFImpactCountAtLeast(0.7)(doc); a.Value != "1" {
		t.Errorf("HDFImpactCountAtLeast(0.7) = %+v; want 1", a)
	}
	if a, _ := HDFImpactPresentAtLeast(0.9)(doc); a.Value != "true" {
		t.Errorf("HDFImpactPresentAtLeast(0.9) = %+v; want true", a)
	}
	if a, _ := HDFImpactPresentAtLeast(0.95)(doc); a.Value != "false" {
		t.Errorf("HDFImpactPresentAtLeast(0.95) = %+v; want false", a)
	}
}

// The headline pins, computed from the REAL gosec fixture and its vendored
// conversion (testdata/gosec.hdf.json): the 7-vs-3 count is Class B, rule
// existence is Class A, and a compliance rate is Class C — all three classes from
// one small scan, fully offline.
func TestClassify_GosecPins(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "gosec.json"))
	if err != nil {
		t.Fatalf("read raw fixture: %v", err)
	}
	hdf, err := os.ReadFile(filepath.Join("testdata", "gosec.hdf.json"))
	if err != nil {
		t.Fatalf("read vendored hdf: %v", err)
	}

	byID := map[string]Candidate{}
	for _, c := range Bank() {
		byID[c.ID] = c
	}

	cases := []struct {
		id            string
		wantClass     Class
		wantRaw       string // "" when unanswerable
		wantHDF       string
		rawAnswerable bool
	}{
		{"gosec-finding-count", ClassB, "7", "3", true},
		{"gosec-rule-present", ClassA, "true", "true", true},
		{"gosec-compliance-rate", ClassC, "", "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			c, ok := byID[tc.id]
			if !ok {
				t.Fatalf("question %s not in Bank()", tc.id)
			}
			got, err := Classify(c.Question, [][]byte{raw}, [][]byte{hdf})
			if err != nil {
				t.Fatalf("classify: %v", err)
			}
			if got.Class != tc.wantClass {
				t.Errorf("class = %s, want %s (raw=%+v hdf=%+v)", got.Class, tc.wantClass, got.RawAnswer, got.HDFAnswer)
			}
			if got.RawAnswer.Answerable != tc.rawAnswerable {
				t.Errorf("raw answerable = %v, want %v", got.RawAnswer.Answerable, tc.rawAnswerable)
			}
			if tc.rawAnswerable && got.RawAnswer.Value != tc.wantRaw {
				t.Errorf("raw answer = %q, want %q", got.RawAnswer.Value, tc.wantRaw)
			}
			if got.HDFAnswer.Value != tc.wantHDF {
				t.Errorf("hdf answer = %q, want %q", got.HDFAnswer.Value, tc.wantHDF)
			}
		})
	}
}

// TestInspecTruth pins the InSpec raw-view answers against the real fixture, and
// — the part that matters — asserts they agree with the HDF view after
// conversion. If a future converter change made InSpec normalization lossy, these
// questions would silently stop being Class A and the study would start grading
// two arms against different keys.
func TestInspecTruth(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/inspec.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		fn   func([]byte) (Answer, error)
		want string
	}{
		{"control count", InspecControlCount, "192"},
		{"compliance rate", InspecComplianceRate, "80"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !got.Answerable || got.Value != tc.want {
				t.Errorf("got %+v, want %q", got, tc.want)
			}
		})
	}
}

// TestCrossFormatTruth pins the cross-format aggregate — HDF's core Heimdall use
// case: three UNLIKE severity vocabularies (gosec HIGH/MEDIUM/LOW, ZAP numeric
// riskcodes, grype Critical..Unknown) asked one question. The raw view must sum
// per-format high-or-above counts over the real fixtures: gosec 0 (all MEDIUM) +
// zap 3 (riskcode 3) + grype 57 (13 Critical + 44 High) = 60.
func TestCrossFormatTruth(t *testing.T) {
	docs := make([][]byte, 0, 3)
	for _, f := range []string{"gosec.json", "zap.json", "grype.json"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "fixtures", f))
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, b)
	}
	got, err := CrossFormatHighSeverityCount(docs)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answerable || got.Value != "60" {
		t.Errorf("raw cross-format high-or-above = %+v, want 60", got)
	}
	if _, err := CrossFormatHighSeverityCount(docs[:2]); err == nil {
		t.Error("want error when a source document is missing")
	}
}

// TestHDFImpactTotalAtLeast exercises the multi-document HDF view: the sum of
// impact>=min counts across several converted documents.
func TestHDFImpactTotalAtLeast(t *testing.T) {
	docA := []byte(`{"baselines":[{"requirements":[{"id":"a","impact":0.9},{"id":"b","impact":0.5}]}]}`)
	docB := []byte(`{"baselines":[{"requirements":[{"id":"c","impact":0.7}]}]}`)
	got, err := HDFImpactTotalAtLeast(0.7)([][]byte{docA, docB})
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "2" {
		t.Errorf("HDFImpactTotalAtLeast(0.7) = %+v, want 2", got)
	}
}

// TestTemporalDiffTruth pins the raw view of the temporal-diff question over the
// real paired fixtures (grype of alpine:3.11 vs alpine:3.12, same grype version
// and DB): 5 distinct vulnerability IDs from the previous scan are gone in the
// current one. Distinct-ID level is deliberate — grype emits one match per
// package instance, so instance-level diffs report churn for CVEs that persist.
func TestTemporalDiffTruth(t *testing.T) {
	prev, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "grype-alpine311.json"))
	if err != nil {
		t.Fatal(err)
	}
	curr, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "grype-alpine312.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := GrypeDistinctFixedCount([][]byte{prev, curr})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answerable || got.Value != "5" {
		t.Errorf("distinct fixed = %+v, want 5", got)
	}
	if _, err := GrypeDistinctFixedCount([][]byte{prev}); err == nil {
		t.Error("want error when a scan is missing")
	}
}

// TestJoinTruth pins the raw view of the SBOM join over the real pair (syft SBOM
// of the SAME image grype scanned): 15 SBOM packages, 7 with vulnerability
// matches, so 8 are vulnerability-free. Sources arrive grype-first.
func TestJoinTruth(t *testing.T) {
	grype, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "grype-alpine312.json"))
	if err != nil {
		t.Fatal(err)
	}
	sbom, err := os.ReadFile(filepath.Join("..", "..", "fixtures", "spdx-alpine312.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := SBOMVulnFreePackageCount([][]byte{grype, sbom})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answerable || got.Value != "8" {
		t.Errorf("vuln-free packages = %+v, want 8", got)
	}
	if _, err := SBOMVulnFreePackageCount([][]byte{grype}); err == nil {
		t.Error("want error when the SBOM is missing")
	}
}

// TestHDFVulnFreePackages exercises both branches of the HDF join view on
// hand-built documents: a system doc that embeds its BOM inventory answers (the
// forward path a future converter activates), and today's reference-only shape
// is unanswerable — which is what makes the live question Class D.
func TestHDFVulnFreePackages(t *testing.T) {
	results := []byte(`{"baselines":[{"requirements":[
		{"id":"Grype/CVE-1","code":"{\"artifact\":{\"name\":\"musl\"}}"},
		{"id":"Grype/CVE-2","code":"{\"artifact\":{\"name\":\"zlib\"}}"}
	]}]}`)
	embedded := []byte(`{"components":[{"boms":[{"document":{"packages":[
		{"name":"musl"},{"name":"zlib"},{"name":"busybox"},{"name":"ssl_client"}
	]}}]}]}`)
	got, err := HDFVulnFreePackageCount([][]byte{results, embedded})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Answerable || got.Value != "2" {
		t.Errorf("embedded-BOM join = %+v, want 2 (busybox, ssl_client)", got)
	}

	refOnly := []byte(`{"components":[{"boms":[{"bomType":"sbom","format":"spdx","ref":"spdx.json"}]}]}`)
	got, err = HDFVulnFreePackageCount([][]byte{results, refOnly})
	if err != nil {
		t.Fatal(err)
	}
	if got.Answerable {
		t.Errorf("reference-only BOM should be unanswerable, got %+v", got)
	}
	if _, err := HDFVulnFreePackageCount([][]byte{results}); err == nil {
		t.Error("want error when the system document is missing")
	}
	if _, err := HDFDistinctFixedCount([][]byte{results}); err == nil {
		t.Error("want error when the diff's second document is missing")
	}
}

// TestGrypeRelatedVulns pins the category-5 pair. The two views must AGREE (45):
// conversion keeps the field, so this is Class A and the HDF arm is graded on it
// rather than excused. If a converter change ever did drop the field, this test
// fails loudly instead of the question silently reclassifying.
func TestGrypeRelatedVulns(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/grype.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := GrypeRelatedVulnCount(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "45" {
		t.Errorf("raw related-vuln count = %q, want 45", got.Value)
	}
}

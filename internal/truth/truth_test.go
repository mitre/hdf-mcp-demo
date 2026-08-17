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
		{"hdf cannot answer -> B", Answered("7"), Unanswerable(), ClassB},
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
	if a, _ := AlwaysUnanswerable(gosec); a.Answerable {
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
			got, err := Classify(c.Question, raw, hdf)
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

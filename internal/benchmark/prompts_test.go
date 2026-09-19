package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	hdfmcpdemo "github.com/mitre/hdf-mcp-demo"
)

// TestEmbeddedQuestionsCoverSkeleton pins the contract between questions.md and
// the Go skeleton: every vetted ID has exactly one prompt, no prompt names an
// ID without ground truth, and Bank therefore cannot panic.
func TestEmbeddedQuestionsCoverSkeleton(t *testing.T) {
	prompts, err := ParsePrompts(strings.NewReader(hdfmcpdemo.QuestionsMD))
	if err != nil {
		t.Fatalf("embedded questions.md does not parse: %v", err)
	}
	want := map[string]bool{}
	for _, q := range skeleton() {
		want[q.ID] = true
	}
	got := map[string]bool{}
	for _, p := range prompts {
		if got[p.ID] {
			t.Errorf("questions.md names %q twice", p.ID)
		}
		got[p.ID] = true
		if !want[p.ID] {
			t.Errorf("questions.md names %q, which has no ground truth in the skeleton", p.ID)
		}
		if strings.TrimSpace(p.Ask) == "" {
			t.Errorf("questions.md gives %q an empty prompt", p.ID)
		}
	}
	for id := range want {
		if !got[id] {
			t.Errorf("questions.md has no prompt for %q", id)
		}
	}
	if bank := Bank(); len(bank) != len(prompts) {
		t.Errorf("Bank() has %d questions, questions.md has %d", len(bank), len(prompts))
	}
}

// TestBankBindsPromptsToSkeleton checks the wording comes from the file and
// everything else from the skeleton, so an edited prompt keeps its answer key.
func TestBankBindsPromptsToSkeleton(t *testing.T) {
	bank, err := BankFrom([]Prompt{{ID: "grype-match-count", Ask: "Count the grype matches."}})
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) != 1 {
		t.Fatalf("got %d questions, want 1", len(bank))
	}
	q := bank[0]
	if q.Ask != "Count the grype matches." {
		t.Errorf("Ask = %q, want the file's wording", q.Ask)
	}
	if q.Kind != KindCount || len(q.Sources) != 1 || q.Sources[0].Fixture != "grype.json" || q.Truth.Raw == nil || len(q.Oracle) == 0 {
		t.Errorf("skeleton fields were not bound: %+v", q)
	}
}

func TestParsePrompts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    []Prompt
		wantErr string
	}{
		{
			name: "wrapped prompt joins to one line; preamble, comments and other headings are ignored",
			in: "# Title\n\nSome preamble that is not a prompt.\n\n<!-- a\nmulti-line comment -->\n" +
				"## first-id\n\n### not a question\nHow many\n  things are   there?\n\n## `second-id`\nYes or no?\n",
			want: []Prompt{{ID: "first-id", Ask: "How many things are   there?"}, {ID: "second-id", Ask: "Yes or no?"}},
		},
		{
			name: "order is the file's order",
			in:   "## b\nB?\n## a\nA?\n",
			want: []Prompt{{ID: "b", Ask: "B?"}, {ID: "a", Ask: "A?"}},
		},
		{name: "duplicate heading", in: "## a\nA?\n## a\nAgain?\n", wantErr: `"a" appears more than once`},
		{name: "heading without prompt", in: "## a\n## b\nB?\n", wantErr: `"a" has no prompt text`},
		{name: "trailing heading without prompt", in: "## a\nA?\n## b\n", wantErr: `"b" has no prompt text`},
		{name: "empty heading", in: "## \nA?\n", wantErr: "has no ID"},
		{name: "no questions", in: "# just a title\nand text\n", wantErr: "no questions found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePrompts(strings.NewReader(tc.in))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d prompts %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("prompt %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestBankFromRejectsUnknownID: a prompt with no ground truth cannot be graded,
// and the error must tell the user which IDs exist rather than only saying no.
func TestBankFromRejectsUnknownID(t *testing.T) {
	_, err := BankFrom([]Prompt{{ID: "made-up", Ask: "How many?"}})
	if err == nil {
		t.Fatal("expected an error for an unknown ID")
	}
	for _, want := range []string{`"made-up"`, "grype-match-count", "inspec-control-count"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %s: %v", want, err)
		}
	}
}

// TestLoadBank covers the file path: a subset in a custom order, and the two
// ways a file can fail (missing, malformed) each naming the file.
func TestLoadBank(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "q.md")
	if err := os.WriteFile(path, []byte("## zap-alert-count\nHow many ZAP alerts?\n\n## gosec-distinct-rules\nDistinct gosec rules?\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bank, err := LoadBank(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank) != 2 || bank[0].ID != "zap-alert-count" || bank[1].ID != "gosec-distinct-rules" {
		t.Errorf("bank = %v, want the file's two questions in its order", bank)
	}
	if bank[0].Ask != "How many ZAP alerts?" {
		t.Errorf("Ask = %q", bank[0].Ask)
	}

	if _, err := LoadBank(filepath.Join(dir, "missing.md")); err == nil || !strings.Contains(err.Error(), "-questions") {
		t.Errorf("missing file: err = %v, want it to name -questions", err)
	}
	bad := filepath.Join(dir, "bad.md")
	if err := os.WriteFile(bad, []byte("## nope\nWhat?\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBank(bad); err == nil || !strings.Contains(err.Error(), "bad.md") || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("unknown ID: err = %v, want it to name the file and the ID", err)
	}
}

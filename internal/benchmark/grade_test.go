package benchmark

import (
	"testing"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

func TestGrade_Count(t *testing.T) {
	key := truth.Answered("3")
	cases := []struct {
		name  string
		reply string
		want  Verdict
	}{
		{"exact answer line", "The scan dedups to three.\nANSWER: 3", Correct},
		{"answer line with word", "ANSWER: 3 distinct rules", Correct},
		{"wrong number", "ANSWER: 7", Wrong},
		{"first int in answer line wins", "ANSWER: 3 (from 7 raw)", Correct},
		{"no answer line, hedging -> abstain", "I cannot determine the exact count from this data.", Abstained},
		{"no answer line, bare number", "There are 3 findings.", Correct},
		{"empty", "", Abstained},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Grade(c.reply, key, KindCount); got != c.want {
				t.Errorf("Grade(%q) = %s, want %s", c.reply, got, c.want)
			}
		})
	}
}

func TestGrade_Bool(t *testing.T) {
	yes := truth.Answered("true")
	no := truth.Answered("false")
	cases := []struct {
		name  string
		reply string
		key   truth.Answer
		want  Verdict
	}{
		{"yes matches true", "ANSWER: yes", yes, Correct},
		{"present matches true", "ANSWER: present", yes, Correct},
		{"no matches false", "ANSWER: no", no, Correct},
		{"not present matches false", "ANSWER: not present", no, Correct},
		{"not present does NOT match true", "ANSWER: not present", yes, Wrong},
		{"yes vs false key", "ANSWER: yes", no, Wrong},
		{"unparseable -> abstain", "ANSWER: it depends on context", yes, Abstained},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Grade(c.reply, c.key, KindBool); got != c.want {
				t.Errorf("Grade(%q) = %s, want %s", c.reply, got, c.want)
			}
		})
	}
}

// A Class-C raw arm gets an unanswerable key: any concrete number is a
// Hallucination (out of remit, not a scored failure); a declination is Abstained.
func TestGrade_UnanswerableKey(t *testing.T) {
	key := truth.Unanswerable()
	if got := Grade("ANSWER: 40", key, KindCount); got != Hallucinated {
		t.Errorf("concrete answer to unanswerable = %s, want Hallucinated", got)
	}
	if got := Grade("I cannot compute a compliance rate from a raw vuln scan. ANSWER: n/a", key, KindCount); got != Abstained {
		t.Errorf("declination to unanswerable = %s, want Abstained", got)
	}
}

// key() must pick the fair grading value per class and intent.
func TestKeySelection(t *testing.T) {
	rawView := truth.Answered("7")
	hdfView := truth.Answered("3")

	classA := Question{Intent: IntentHDF}
	if got := classA.key(truth.ClassA, rawView, hdfView, ArmRaw); got.Value != "3" {
		t.Errorf("Class A raw key = %q, want the shared value 3", got.Value)
	}

	bHDF := Question{Intent: IntentHDF}
	if got := bHDF.key(truth.ClassB, rawView, hdfView, ArmRaw); got.Value != "3" {
		t.Errorf("Class B intent=HDF raw-arm key = %q, want 3 (both arms graded to intent)", got.Value)
	}
	bRaw := Question{Intent: IntentRaw}
	if got := bRaw.key(truth.ClassB, rawView, hdfView, ArmHDF); got.Value != "7" {
		t.Errorf("Class B intent=raw hdf-arm key = %q, want 7", got.Value)
	}

	c := Question{Intent: IntentHDF}
	if got := c.key(truth.ClassC, truth.Unanswerable(), hdfView, ArmRaw); got.Answerable {
		t.Errorf("Class C raw-arm key should be unanswerable, got %+v", got)
	}
	if got := c.key(truth.ClassC, truth.Unanswerable(), hdfView, ArmHDF); got.Value != "3" {
		t.Errorf("Class C hdf-arm key = %q, want the HDF value 3", got.Value)
	}
}

// TestGrade_DecliningReplyAbstains pins the rule grade.go states about itself:
// prefer a false Abstained over a false anything-else. A reply that declines
// asserts nothing, so it must never be recorded as a confident wrong answer —
// that reads downstream as a capability failure when it is usually a broken
// tool call. The count path already guarded this; the bool path did not.
func TestGrade_DecliningReplyAbstains(t *testing.T) {
	answerable := truth.Answered("true")
	for _, tc := range []struct {
		name  string
		reply string
	}{
		{"cannot answer, mentions yes/no", "I could not open the document, so I cannot give a definitive yes/no answer."},
		{"tool failed", "The tool returned an error, so I am unable to determine whether it is present."},
		{"no data", "I don't know — there is no data available to answer this."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Grade(tc.reply, answerable, KindBool); got != Abstained {
				t.Errorf("Grade(%q) = %s, want %s", tc.reply, got, Abstained)
			}
		})
	}
}

// TestGrade_AnswerLineBeatsTrailingCommentary pins that an ANSWER line is read
// as its value, not as a bag of words. Models routinely echo the system prompt's
// format template after the value; that echo carries both "yes" and "no" and was
// outvoting the answer the model actually gave.
func TestGrade_AnswerLineBeatsTrailingCommentary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		key   truth.Answer
		kind  Kind
		want  Verdict
	}{
		{"echoed template after yes", "ANSWER: yes — where <value> is a single number, or a single yes/no, and nothing else on that line.", truth.Answered("true"), KindBool, Correct},
		{"echoed template after no", "ANSWER: no — where <value> is a single number, or a single yes/no, and nothing else on that line.", truth.Answered("false"), KindBool, Correct},
		{"prose justification after yes", "ANSWER: yes — the CVE identifier appears in the grype.json scan.", truth.Answered("true"), KindBool, Correct},
		{"echoed template after count", "ANSWER: 3 — where <value> is a single number, and nothing else on that line.", truth.Answered("3"), KindCount, Correct},
		{"wrong value still wrong", "ANSWER: no — where <value> is a single yes/no.", truth.Answered("true"), KindBool, Wrong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Grade(tc.reply, tc.key, tc.kind); got != tc.want {
				t.Errorf("Grade(%q) = %s, want %s", tc.reply, got, tc.want)
			}
		})
	}
}

// TestGrade_ContradictoryReplyAbstains covers the whole-reply fallback: when a
// reply carries both signals and no ANSWER line to disambiguate, the honest
// verdict is Abstained. Resolving silently to "no" (the old first-match-wins
// order) invented a confident answer the model never gave.
func TestGrade_ContradictoryReplyAbstains(t *testing.T) {
	reply := "Results are mixed: yes for the openssl package, no for zlib."
	if got := Grade(reply, truth.Answered("true"), KindBool); got != Abstained {
		t.Errorf("Grade(contradictory) = %s, want %s", got, Abstained)
	}
}

// TestGrade_NegationsStillNegative guards the fix from over-correcting: the
// negated phrasings contain positive words ("not present" contains "present"),
// and value-only parsing must not re-read them as affirmations.
func TestGrade_NegationsStillNegative(t *testing.T) {
	for _, reply := range []string{
		"ANSWER: not present",
		"ANSWER: it is not found in the scan",
		"ANSWER: false",
		"ANSWER: no",
	} {
		if got := Grade(reply, truth.Answered("false"), KindBool); got != Correct {
			t.Errorf("Grade(%q) with key=false = %s, want %s", reply, got, Correct)
		}
	}
}

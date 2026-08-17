package benchmark

import (
	"regexp"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// Kind is how a free-text model answer is reduced to a comparable value.
type Kind string

const (
	KindCount Kind = "count" // a non-negative integer
	KindBool  Kind = "bool"  // yes/no · present/absent · true/false
)

// Verdict is the outcome of grading one arm's answer against a ground-truth key.
type Verdict string

const (
	Correct   Verdict = "correct"   // extracted answer matches the key
	Wrong     Verdict = "wrong"     // extracted a concrete answer, but not the key
	Abstained Verdict = "abstained" // declined / no extractable answer
)

// Grading is intentionally lenient and therefore imperfect: it parses a final
// "ANSWER: <value>" line the benchmark asks the model to emit, falling back to a
// scan of the whole reply. It cannot read intent behind prose, so a model that
// buries the right number in a sentence without the ANSWER line may be marked
// Abstained/Wrong. This is a known limitation, surfaced in the report footer;
// the alternative (an LLM judge) reintroduces cost and its own error. Prefer
// false-Abstained over false-Correct: never credit an answer we didn't clearly
// parse.

var (
	answerLineRE = regexp.MustCompile(`(?i)answer\s*[:\-]\s*(.+)`)
	intRE        = regexp.MustCompile(`-?\d+`)
	abstainRE    = regexp.MustCompile(`(?i)\b(cannot|can't|can not|unable|not able|no\b.*\b(data|info|information|way)|not (?:enough|sufficient)|don't know|do not know|unknown|indeterminate|n/?a)\b`)
)

// finalAnswer returns the text after the last "ANSWER:" marker, or "" if none.
func finalAnswer(reply string) string {
	ms := answerLineRE.FindAllStringSubmatch(reply, -1)
	if len(ms) == 0 {
		return ""
	}
	return strings.TrimSpace(ms[len(ms)-1][1])
}

// Grade reduces reply to a value per kind and compares it to key. When key is
// unanswerable (the raw arm of a Class-C question, which has no correct answer),
// a concrete answer is Wrong (a hallucination) and a declination is Abstained.
func Grade(reply string, key truth.Answer, kind Kind) Verdict {
	scope := finalAnswer(reply)
	whole := scope == ""
	if whole {
		scope = reply
	}

	switch kind {
	case KindBool:
		got, ok := parseBool(scope)
		if !ok {
			return Abstained
		}
		if !key.Answerable {
			return Wrong // asserted a boolean where none is answerable
		}
		if got == (key.Value == "true") {
			return Correct
		}
		return Wrong
	default: // KindCount
		got, ok := firstInt(scope)
		if !ok {
			return Abstained
		}
		// A bare integer pulled from the whole reply (no ANSWER line) is weak
		// evidence; only trust it as an abstention signal, not a value, when the
		// text is hedging.
		if whole && abstainRE.MatchString(reply) {
			return Abstained
		}
		if !key.Answerable {
			return Wrong // produced a number where none is answerable
		}
		if got == key.Value {
			return Correct
		}
		return Wrong
	}
}

// firstInt returns the first integer token in s.
func firstInt(s string) (string, bool) {
	m := intRE.FindString(s)
	if m == "" {
		return "", false
	}
	return m, true
}

// parseBool maps common affirmative/negative phrasings to a boolean. Negations
// are tested first so "not present"/"not found" don't match the affirmative
// "present"/"found".
func parseBool(s string) (bool, bool) {
	l := strings.ToLower(s)
	switch {
	case containsWord(l, "no"), containsWord(l, "false"), containsWord(l, "absent"),
		strings.Contains(l, "not present"), strings.Contains(l, "not found"), strings.Contains(l, "isn't"), strings.Contains(l, "is not"):
		return false, true
	case containsWord(l, "yes"), containsWord(l, "true"), containsWord(l, "present"), containsWord(l, "found"):
		return true, true
	}
	return false, false
}

// containsWord reports whether word appears as a whole word in s.
func containsWord(s, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(s)
}

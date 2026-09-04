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
	Correct      Verdict = "correct"      // extracted answer matches the key
	Wrong        Verdict = "wrong"        // extracted a concrete answer, but not the key
	Abstained    Verdict = "abstained"    // declined / no extractable answer
	Failed       Verdict = "failed"       // the arm errored or timed out (e.g. over-context) before answering
	Hallucinated Verdict = "hallucinated" // gave a concrete answer to a question its data view cannot answer (out of remit)
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

// finalAnswer returns the value of the last "ANSWER:" marker, or "" if none.
//
// The value is taken up to the first commentary separator. The prompt asks for
// "ANSWER: <value>" and models routinely append a justification — or echo the
// prompt's own format template, which contains the words "yes/no" and so
// contributed a spurious negative that outvoted the answer actually given.
// Reading the value rather than the whole line makes the parse depend on what
// the model answered, not on what it said afterwards.
func finalAnswer(reply string) string {
	ms := answerLineRE.FindAllStringSubmatch(reply, -1)
	if len(ms) == 0 {
		return ""
	}
	return answerValue(ms[len(ms)-1][1])
}

// answerValue trims trailing commentary from an answer line.
func answerValue(s string) string {
	for _, sep := range []string{"—", "–", " - ", "\n", ";", ","} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// Grade reduces reply to a value per kind and compares it to key. When key is
// unanswerable (the raw arm of an hdf-only question, which has no correct answer
// and is out of remit), a concrete answer is Hallucinated and a declination is
// Abstained — neither counts toward scored accuracy.
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
		// A boolean read out of the whole reply (no ANSWER line) is weak evidence,
		// exactly as for counts: when the text is hedging or reporting an inability,
		// the model declined rather than answered. Recording that as Wrong invents a
		// confident answer it never gave — and reads downstream as a capability
		// failure when the usual cause is a tool call that did not work.
		if whole && abstainRE.MatchString(reply) {
			return Abstained
		}
		if !key.Answerable {
			return Hallucinated // asserted a boolean where none is answerable (out of remit)
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
			return Hallucinated // produced a number where none is answerable (out of remit)
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
// parseBool reduces a reply to a boolean. It reports ok=false when the text
// carries no signal OR carries both — an ambiguous reply is not an answer, and
// silently resolving it to the first match found (which was always the negative)
// credits the model with a position it did not take.
func parseBool(s string) (bool, bool) {
	l := strings.ToLower(s)

	// Negated phrasings embed their own positive word ("not present" contains
	// "present"), so consume them first and search what remains for affirmations.
	neg := false
	for _, p := range []string{"not present", "not found", "not detected", "isn't", "is not"} {
		if strings.Contains(l, p) {
			neg = true
			l = strings.ReplaceAll(l, p, " ")
		}
	}
	for _, w := range []string{"no", "false", "absent"} {
		if containsWord(l, w) {
			neg = true
		}
	}
	pos := false
	for _, w := range []string{"yes", "true", "present", "found"} {
		if containsWord(l, w) {
			pos = true
		}
	}

	switch {
	case neg && pos:
		return false, false // contradictory — not an answer
	case neg:
		return false, true
	case pos:
		return true, true
	}
	return false, false
}

// containsWord reports whether word appears as a whole word in s.
func containsWord(s, word string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`).MatchString(s)
}

// Package truth computes, for a candidate benchmark question, the answer over the
// RAW scanner view and the answer over the normalized HDF view — independently and
// from the data itself — then classifies the question by whether those two answers
// agree:
//
//	A  answer-preserving      raw and HDF agree; a fair apples-to-apples fact
//	B  normalization-divergent raw and HDF disagree, each correct for its own view
//	C  HDF-native             the raw view cannot natively answer; only HDF can
//
// Class B is the gosec 7-vs-3 case: the raw scan emits 7 findings, conversion
// deduplicates them into 3 rule-level requirements, and neither number is wrong.
// Grading a Class-B question to a single ground truth would score that divergence
// as an accuracy failure (ADR-0002), so the class is computed here rather than
// asserted, and the grader (Phase 3d) uses the class to pick the fair reference.
//
// Ground truth is derived from the data only. It is never read back from the model
// arm under test — that would let a confident-but-wrong arm define its own key.
package truth

import "fmt"

// Class is a question's fairness classification (see the package doc).
type Class string

const (
	ClassA Class = "A" // answer-preserving: raw == HDF
	ClassB Class = "B" // normalization-divergent: raw != HDF, both valid for their view
	ClassC Class = "C" // HDF-native: raw cannot natively answer, HDF can
)

// Answer is a question's computed answer over one data view. Value is the
// canonical, comparable form (a count as its decimal string, existence as
// "true"/"false", etc.). Answerable is false when the view cannot natively
// express the answer at all — a vuln scanner has no notion of a compliance pass
// rate — which is what distinguishes Class C from a mere disagreement.
type Answer struct {
	Value      string
	Answerable bool
}

// Answered is an answer the view can express.
func Answered(value string) Answer { return Answer{Value: value, Answerable: true} }

// Unanswerable is the answer for a view that cannot natively express the fact.
func Unanswerable() Answer { return Answer{Answerable: false} }

// AlwaysUnanswerable is a ground-truth function for a view that can never express
// the fact (e.g. a compliance pass rate over a raw vuln scan). It ignores its
// input and marks the answer unanswerable, which drives Class C.
func AlwaysUnanswerable(_ []byte) (Answer, error) { return Unanswerable(), nil }

// Question binds a prompt to the two functions that compute its ground truth: Raw
// parses the raw scanner fixture, HDF reads the converted HDF document. The
// classifier calls both; neither ever sees a model's answer.
type Question struct {
	ID     string
	Prompt string
	Raw    func(rawFixture []byte) (Answer, error)
	HDF    func(hdfDoc []byte) (Answer, error)
}

// Result is a classified question: the two computed answers and the class implied
// by their agreement.
type Result struct {
	ID        string
	Prompt    string
	RawAnswer Answer
	HDFAnswer Answer
	Class     Class
}

// Classify computes the raw-view and HDF-view answers for q and tags the result.
func Classify(q Question, rawFixture, hdfDoc []byte) (Result, error) {
	raw, err := q.Raw(rawFixture)
	if err != nil {
		return Result{}, fmt.Errorf("%s: raw answer: %w", q.ID, err)
	}
	hdf, err := q.HDF(hdfDoc)
	if err != nil {
		return Result{}, fmt.Errorf("%s: hdf answer: %w", q.ID, err)
	}
	return Result{
		ID:        q.ID,
		Prompt:    q.Prompt,
		RawAnswer: raw,
		HDFAnswer: hdf,
		Class:     classOf(raw, hdf),
	}, nil
}

// classOf is the classification rule. Order matters: an answer the raw view cannot
// express is Class C (HDF adds a capability) regardless of the HDF value; among
// answers both views express, equality is Class A and disagreement is Class B.
func classOf(raw, hdf Answer) Class {
	if !raw.Answerable && hdf.Answerable {
		return ClassC
	}
	if raw.Answerable && hdf.Answerable && raw.Value == hdf.Value {
		return ClassA
	}
	return ClassB
}

package benchmark

import "github.com/mitre/hdf-mcp-demo/internal/truth"

// IntentView selects which computed ground-truth value is the fair grading key for
// a Class-B (normalization-divergent) question — the whole point of ADR-0002's
// bidirectional rule. It is ignored for Class A (the two views agree) and Class C
// (only the HDF view answers).
type IntentView string

const (
	IntentHDF IntentView = "hdf" // the rule-level / normalized value is the intended answer
	IntentRaw IntentView = "raw" // the raw finding-volume value is the intended answer
)

// Question is one graded benchmark item: the natural-language ask, the fixture it
// reads, the ground-truth functions that compute each view's answer (reused from
// the truth package), how to grade a free-text reply, and — for Class B — which
// view's value is intended.
type Question struct {
	ID      string
	Ask     string // natural-language question; the harness appends the file name per arm
	Fixture string // raw fixture filename (under fixtures/)
	From    string // hdf_convert source format
	HDFName string // converted filename produced under the run root
	Kind    Kind
	Intent  IntentView // Class B only
	Truth   truth.Question
}

// Bank is the vetted question set (ADR-0002 Phase 3c). It spans all three classes
// and — critically — carries the Class-B gosec count in BOTH directions so the
// study never reports only the HDF-flattering side:
//
//   - gosec-distinct-rules  (B, intent=HDF): "distinct rule violations" → key 3;
//     HDF wins, the raw arm must manually deduplicate.
//   - gosec-total-findings  (B, intent=raw): "total findings emitted" → key 7;
//     raw wins, HDF's dedup makes it the wrong number for this intent.
//   - grype-match-count     (A): grype doesn't dedup, so raw==HDF (89) — a fair
//     apples-to-apples count, the honest contrast to gosec's divergence.
//   - grype-cve-present     (A): existence survives normalization.
//   - grype-compliance-rate (C): a vuln scan has no native pass rate; HDF adds it,
//     and we measure how often the raw arm invents one.
func Bank() []Question {
	gosecTruth := truth.Question{Raw: truth.GosecFindingCount, HDF: truth.HDFRequirementCount}
	return []Question{
		{
			ID:   "gosec-distinct-rules",
			Ask:  "How many DISTINCT rule violations (unique rule IDs) are in the gosec SAST scan?",
			Kind: KindCount, Intent: IntentHDF,
			Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json",
			Truth: gosecTruth,
		},
		{
			ID:   "gosec-total-findings",
			Ask:  "How many total findings did the gosec SAST scanner emit (the raw count of individual finding entries, before any de-duplication)?",
			Kind: KindCount, Intent: IntentRaw,
			Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json",
			Truth: gosecTruth,
		},
		{
			ID:   "grype-match-count",
			Ask:  "How many vulnerability matches are in the grype scan?",
			Kind: KindCount, Intent: IntentHDF, // A: both views agree, intent is moot
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
			Truth: truth.Question{Raw: truth.GrypeMatchCount, HDF: truth.HDFRequirementCount},
		},
		{
			ID:   "grype-cve-present",
			Ask:  "Is CVE-2021-36159 present in the grype scan? Answer yes or no.",
			Kind: KindBool, Intent: IntentHDF,
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
			Truth: truth.Question{Raw: truth.GrypeCVEPresent("CVE-2021-36159"), HDF: truth.HDFRequirementPresent("CVE-2021-36159")},
		},
		{
			ID:   "grype-compliance-rate",
			Ask:  "What percentage of the grype scan is passing (the compliance pass rate)? Answer with a whole-number percentage.",
			Kind: KindCount, Intent: IntentHDF,
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
			Truth: truth.Question{Raw: truth.AlwaysUnanswerable, HDF: truth.HDFComplianceRate},
		},
		{
			ID:   "grype-has-critical",
			Ask:  "Does the grype scan contain any Critical-severity finding? Answer yes or no.",
			Kind: KindBool, Intent: IntentHDF,
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
			Truth: truth.Question{Raw: truth.GrypeSeverityPresent("Critical"), HDF: truth.HDFImpactPresentAtLeast(0.9)},
		},
		{
			ID:   "zap-alert-count",
			Ask:  "How many alerts are in the ZAP (DAST) scan?",
			Kind: KindCount, Intent: IntentHDF, // A: ZAP does not dedup, so raw==HDF
			Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json",
			Truth: truth.Question{Raw: truth.ZapAlertCount, HDF: truth.HDFRequirementCount},
		},
		{
			ID:   "zap-high-severity-count",
			Ask:  "How many high-risk alerts are in the ZAP scan?",
			Kind: KindCount, Intent: IntentHDF,
			Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json",
			Truth: truth.Question{Raw: truth.ZapHighCount, HDF: truth.HDFImpactCountAtLeast(0.7)},
		},
	}
}

// key returns the fair grading key for an arm given the question's class and
// intent. Class A grades both arms to the shared value; Class B grades both to the
// intended view; Class C grades the HDF arm to the HDF value and hands the raw arm
// an unanswerable key (so any concrete raw answer is scored as a hallucination).
func (q Question) key(class truth.Class, rawView, hdfView truth.Answer, arm Arm) truth.Answer {
	switch class {
	case truth.ClassA:
		return hdfView // == rawView
	case truth.ClassC:
		if arm == ArmRaw {
			return truth.Unanswerable() // raw out of remit (hdf-native)
		}
		return hdfView
	case truth.ClassD:
		if arm == ArmHDF {
			return truth.Unanswerable() // hdf out of remit (raw-only field)
		}
		return rawView
	default: // Class B — grade to intent, same key for both arms
		if q.Intent == IntentRaw {
			return rawView
		}
		return hdfView
	}
}

// Arm identifies which side of the study produced an answer.
type Arm string

const (
	ArmRaw Arm = "raw-file"
	ArmHDF Arm = "hdf-mcp"
)

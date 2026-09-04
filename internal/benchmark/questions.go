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
// Source is one document a question reads: the raw fixture and the normalized
// document produced from it. Most questions have exactly one; a cross-document
// question (an SBOM joined against a vuln scan, a scan diffed against its
// predecessor) declares several, and both arms are told about all of them.
type Source struct {
	Fixture string // raw fixture filename (under fixtures/)
	From    string // hdf_convert source format; empty when CLIPrep is set
	HDFName string // normalized filename produced under the run root
	// CLIPrep normalizes via the shipped hdf CLI instead of the hdf_convert MCP
	// tool, as argv with {raw}/{hdf} placeholders. SPDX becomes an HDF *System*
	// document through `hdf system create`, which is not an hdf_convert path.
	CLIPrep []string
}

// OracleCall is one hand-written, ideal-play MCP tool invocation — the bookend
// the ceiling analysis assumes. It is executed model-free; its response tokens
// are the hdfOracle bookend, and the gated oracle test pins its response against
// the question's computed ground truth so an "optimal call" that does not
// actually answer the question cannot ship.
type OracleCall struct {
	Tool string
	Args map[string]any
}

type Question struct {
	ID      string
	Ask     string   // natural-language question; the harness appends the file names per arm
	Sources []Source // documents the question reads, in the order both arms are told about them
	Kind    Kind
	Intent  IntentView // Class B only
	Truth   truth.Question
	// Exactly one of Oracle / OracleUnreachable is set: either the ideal-play
	// call(s) for the bounded surface, or the reason no bounded call can answer
	// (which is itself a reported finding, not an excuse).
	Oracle            []OracleCall
	OracleUnreachable string
}

// Primary is the question's first source — the one an ad-hoc (convert-on-demand)
// prompt names.
func (q Question) Primary() Source {
	if len(q.Sources) == 0 {
		return Source{}
	}
	return q.Sources[0]
}

// rawNames and hdfNames list the question's filenames for prompt construction.
func (q Question) rawNames() []string {
	out := make([]string, 0, len(q.Sources))
	for _, s := range q.Sources {
		out = append(out, s.Fixture)
	}
	return out
}

func (q Question) hdfNames() []string {
	out := make([]string, 0, len(q.Sources))
	for _, s := range q.Sources {
		out = append(out, s.HDFName)
	}
	return out
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
//   - inspec-control-count / inspec-compliance-rate (A): the same two shapes over a
//     1.2MB compliance run — the large-document regime, where the raw arm must
//     actually work to find what a bounded HDF response returns directly.
func Bank() []Question {
	gosecTruth := truth.Question{Raw: truth.Primary(truth.GosecFindingCount), HDF: truth.Primary(truth.HDFRequirementCount)}
	return []Question{
		{
			ID:   "gosec-distinct-rules",
			Ask:  "How many DISTINCT rule violations (unique rule IDs) are in the gosec SAST scan?",
			Kind: KindCount, Intent: IntentHDF,
			Sources: []Source{{Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json"}},
			Truth:   gosecTruth,
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "gosec.hdf.json"}, "limit": 1}}},
		},
		{
			ID:   "gosec-total-findings",
			Ask:  "How many total findings did the gosec SAST scanner emit (the raw count of individual finding entries, before any de-duplication)?",
			Kind: KindCount, Intent: IntentRaw,
			Sources:           []Source{{Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json"}},
			Truth:             gosecTruth,
			OracleUnreachable: "result-level finding volume; the read surface projects requirement counts only",
		},
		{
			ID:   "grype-match-count",
			Ask:  "How many vulnerability matches are in the grype scan?",
			Kind: KindCount, Intent: IntentHDF, // A: both views agree, intent is moot
			Sources: []Source{{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.GrypeMatchCount), HDF: truth.Primary(truth.HDFRequirementCount)},
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "grype.hdf.json"}, "limit": 1}}},
		},
		{
			ID:   "grype-cve-present",
			Ask:  "Is CVE-2021-36159 present in the grype scan? Answer yes or no.",
			Kind: KindBool, Intent: IntentHDF,
			Sources: []Source{{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.GrypeCVEPresent("CVE-2021-36159")), HDF: truth.Primary(truth.HDFRequirementPresent("CVE-2021-36159"))},
			// search, not id: converters namespace requirement IDs (Grype/CVE-…),
			// and searching the bare CVE is how a real ideal caller would ask.
			Oracle: []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "grype.hdf.json"}, "search": "CVE-2021-36159", "limit": 1}}},
		},
		{
			ID:   "grype-compliance-rate",
			Ask:  "What percentage of the grype scan is passing (the compliance pass rate)? Answer with a whole-number percentage.",
			Kind: KindCount, Intent: IntentHDF,
			Sources: []Source{{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"}},
			Truth:   truth.Question{Raw: truth.AlwaysUnanswerable, HDF: truth.Primary(truth.HDFComplianceRate)},
			Oracle:  []OracleCall{{Tool: "hdf_compliance", Args: map[string]any{"source": map[string]any{"path": "grype.hdf.json"}}}},
		},
		{
			ID:   "grype-has-critical",
			Ask:  "Does the grype scan contain any Critical-severity finding? Answer yes or no.",
			Kind: KindBool, Intent: IntentHDF,
			Sources: []Source{{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.GrypeSeverityPresent("Critical")), HDF: truth.Primary(truth.HDFImpactPresentAtLeast(0.9))},
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "grype.hdf.json"}, "impact": ">=0.9", "limit": 1}}},
		},
		// Category 5: the case where the HDF arm is expected to LOSE. grype records
		// relatedVulnerabilities; conversion preserves it verbatim in the
		// requirement's `code`, so the document can answer (Class A) — but no HDF
		// read tool projects `code`, so the arm must succeed through the bounded
		// surface or fail. Graded, not excused: see HDFRelatedVulnCountFromCode.
		{
			ID:   "grype-related-vulns",
			Ask:  "How many vulnerability matches in the grype scan list at least one related vulnerability?",
			Kind: KindCount, Intent: IntentHDF,
			Sources:           []Source{{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"}},
			Truth:             truth.Question{Raw: truth.Primary(truth.GrypeRelatedVulnCount), HDF: truth.Primary(truth.HDFRelatedVulnCountFromCode)},
			OracleUnreachable: "preserved verbatim in the requirement's code field, which no read tool projects",
		},
		{
			ID:   "zap-alert-count",
			Ask:  "How many alerts are in the ZAP (DAST) scan?",
			Kind: KindCount, Intent: IntentHDF, // A: ZAP does not dedup, so raw==HDF
			Sources: []Source{{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.ZapAlertCount), HDF: truth.Primary(truth.HDFRequirementCount)},
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "zap.hdf.json"}, "limit": 1}}},
		},
		// The InSpec pair is the large-document case (1.2MB raw). Everything else in
		// the bank is a small-to-medium scan, where a bounded tool response has
		// little room to beat grep — so without these the study never exercises the
		// regime normalization is actually for. InSpec is already rule-shaped, so
		// both views agree and the comparison stays apples-to-apples.
		{
			ID:   "inspec-control-count",
			Ask:  "How many controls are in the InSpec compliance run?",
			Kind: KindCount, Intent: IntentHDF, // A: InSpec is rule-shaped already, so raw==HDF
			Sources: []Source{{Fixture: "inspec.json", From: "hdf", HDFName: "inspec.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.InspecControlCount), HDF: truth.Primary(truth.HDFRequirementCount)},
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "inspec.hdf.json"}, "limit": 1}}},
		},
		{
			ID:   "inspec-compliance-rate",
			Ask:  "What percentage of the InSpec compliance run is passing (the pass rate over all test results)? Answer with a whole-number percentage.",
			Kind: KindCount, Intent: IntentHDF, // A: a compliance run states pass/fail natively
			Sources: []Source{{Fixture: "inspec.json", From: "hdf", HDFName: "inspec.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.InspecComplianceRate), HDF: truth.Primary(truth.HDFComplianceRate)},
			Oracle:  []OracleCall{{Tool: "hdf_compliance", Args: map[string]any{"source": map[string]any{"path": "inspec.hdf.json"}}}},
		},
		{
			ID:   "zap-high-severity-count",
			Ask:  "How many high-risk alerts are in the ZAP scan?",
			Kind: KindCount, Intent: IntentHDF,
			Sources: []Source{{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"}},
			Truth:   truth.Question{Raw: truth.Primary(truth.ZapHighCount), HDF: truth.Primary(truth.HDFImpactCountAtLeast(0.7))},
			Oracle:  []OracleCall{{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "zap.hdf.json"}, "impact": ">=0.7", "limit": 1}}},
		},
		// The cross-format aggregate is HDF's core use case (the Heimdall workflow):
		// one question over three UNLIKE severity vocabularies — gosec's
		// LOW/MEDIUM/HIGH, ZAP's numeric riskcodes, grype's Critical..Unknown. The
		// raw arm must discover and reconcile all three itself; the HDF arm filters
		// one normalized impact scale three times. Both views agree on the real
		// fixtures (0 + 3 + 57 = 60), so the vocabulary mapping itself is what is
		// being trusted — and the truth pins fail loudly if a converter ever breaks
		// band alignment.
		{
			ID:   "cross-format-high-count",
			Ask:  "Across the gosec (SAST), ZAP (DAST), and grype (vulnerability) scans together, how many findings are high severity or above?",
			Kind: KindCount, Intent: IntentHDF,
			Sources: []Source{
				{Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json"},
				{Fixture: "zap.json", From: "zap", HDFName: "zap.hdf.json"},
				{Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json"},
			},
			Truth: truth.Question{Raw: truth.CrossFormatHighSeverityCount, HDF: truth.HDFImpactTotalAtLeast(0.7)},
			Oracle: []OracleCall{
				{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "gosec.hdf.json"}, "impact": ">=0.7", "limit": 1}},
				{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "zap.hdf.json"}, "impact": ">=0.7", "limit": 1}},
				{Tool: "hdf_query", Args: map[string]any{"source": map[string]any{"path": "grype.hdf.json"}, "impact": ">=0.7", "limit": 1}},
			},
		},
		// The temporal diff runs over a REAL pair: grype scans of alpine:3.11 and
		// alpine:3.12 captured with one grype version and DB (see
		// fixtures/PROVENANCE.md). Distinct-ID level is deliberate — grype emits one
		// match per package instance, so an instance-level diff reports churn for
		// CVEs that persist across version bumps.
		{
			ID: "grype-fixed-vulns",
			Ask: "Two grype scans of the same container image are provided: the FIRST named file is the previous scan and the SECOND is the current scan. " +
				"How many DISTINCT vulnerability IDs from the previous scan are no longer present in the current scan?",
			Kind: KindCount, Intent: IntentHDF, // A: requirement IDs are the vulnerability IDs
			Sources: []Source{
				{Fixture: "grype-alpine311.json", From: "grype", HDFName: "grype-alpine311.hdf.json"},
				{Fixture: "grype-alpine312.json", From: "grype", HDFName: "grype-alpine312.hdf.json"},
			},
			Truth:  truth.Question{Raw: truth.GrypeDistinctFixedCount, HDF: truth.HDFDistinctFixedCount},
			Oracle: []OracleCall{{Tool: "hdf_diff", Args: map[string]any{"from": map[string]any{"path": "grype-alpine311.hdf.json"}, "to": map[string]any{"path": "grype-alpine312.hdf.json"}}}},
		},
		// The SBOM×vuln join is the bank's genuine Class D (raw-only): the syft
		// SBOM covers the SAME image the grype scan does, but `hdf system create`
		// carries the SBOM as a reference (boms[].ref) with no embedded inventory,
		// so the HDF view does not contain the join key at all. The raw arm must
		// actually join two files; the HDF arm is out of remit and is measured on
		// hallucinate-vs-abstain.
		{
			ID: "sbom-vuln-free-packages",
			Ask: "An SPDX SBOM inventories every package of the same container image the grype scan covers: the FIRST named file is the vulnerability scan and the SECOND is the SBOM. " +
				"How many of the SBOM's packages have NO vulnerability matches in the grype scan?",
			Kind: KindCount, Intent: IntentRaw, // D: only the raw view holds the join key
			Sources: []Source{
				{Fixture: "grype-alpine312.json", From: "grype", HDFName: "grype-alpine312.hdf.json"},
				{Fixture: "spdx-alpine312.json", HDFName: "spdx-alpine312.system.hdf.json",
					CLIPrep: []string{"system", "create", "{raw}", "--from", "spdx", "-o", "{hdf}"}},
			},
			Truth:             truth.Question{Raw: truth.SBOMVulnFreePackageCount, HDF: truth.HDFVulnFreePackageCount},
			OracleUnreachable: "the join key (package inventory) is carried as a BOM reference, not embedded — no HDF document or read tool holds it",
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

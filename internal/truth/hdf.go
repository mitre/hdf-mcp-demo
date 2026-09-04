package truth

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// hdfDoc is the subset of an HDF Results document the classifier reads: the
// rule-level requirements and their per-site results. Conversion groups raw
// findings that share a rule into one requirement (gosec 7→3), so counting
// requirements answers "distinct rule violations" while counting results answers
// "raw finding volume".
type hdfDoc struct {
	Baselines []struct {
		Requirements []hdfRequirement `json:"requirements"`
	} `json:"baselines"`
}

type hdfRequirement struct {
	ID      string  `json:"id"`
	Impact  float64 `json:"impact"`
	Code    string  `json:"code"` // verbatim source finding (converter passthrough)
	Results []struct {
		Status string `json:"status"`
	} `json:"results"`
}

func parseHDF(b []byte) (hdfDoc, error) {
	var d hdfDoc
	if err := json.Unmarshal(b, &d); err != nil {
		return hdfDoc{}, fmt.Errorf("parse hdf: %w", err)
	}
	return d, nil
}

// hdfRequirements flattens the requirements across all baselines.
func hdfRequirements(d hdfDoc) []hdfRequirement {
	var out []hdfRequirement
	for _, b := range d.Baselines {
		out = append(out, b.Requirements...)
	}
	return out
}

// HDFRequirementCount is the number of rule-level requirements — the deduplicated
// count that diverges from the raw finding total for a scanner like gosec.
func HDFRequirementCount(b []byte) (Answer, error) {
	d, err := parseHDF(b)
	if err != nil {
		return Answer{}, err
	}
	return Answered(strconv.Itoa(len(hdfRequirements(d)))), nil
}

// HDFRequirementPresent reports whether any requirement id equals or ends with
// the given rule/CVE token. Converters may namespace ids (e.g. "Grype/CVE-…"),
// so a suffix match after the last '/' is accepted alongside an exact match.
func HDFRequirementPresent(token string) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		d, err := parseHDF(b)
		if err != nil {
			return Answer{}, err
		}
		for _, r := range hdfRequirements(d) {
			if r.ID == token || lastSegment(r.ID) == token {
				return Answered("true"), nil
			}
		}
		return Answered("false"), nil
	}
}

// HDFComplianceRate is passed results / total results as a whole-number percent —
// a status rollup the raw scanner views cannot natively produce (Class C).
func HDFComplianceRate(b []byte) (Answer, error) {
	d, err := parseHDF(b)
	if err != nil {
		return Answer{}, err
	}
	var passed, total int
	for _, r := range hdfRequirements(d) {
		for _, res := range r.Results {
			total++
			if res.Status == "passed" {
				passed++
			}
		}
	}
	if total == 0 {
		return Answered("0"), nil
	}
	return Answered(strconv.Itoa(passed * 100 / total)), nil
}

// HDFImpactCountAtLeast counts requirements whose impact is at least threshold —
// the normalized-severity analogue of a raw scanner's high-severity count.
func HDFImpactCountAtLeast(threshold float64) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		d, err := parseHDF(b)
		if err != nil {
			return Answer{}, err
		}
		n := 0
		for _, r := range hdfRequirements(d) {
			if r.Impact >= threshold {
				n++
			}
		}
		return Answered(strconv.Itoa(n)), nil
	}
}

// HDFDistinctFixedCount is the HDF view of the temporal diff: distinct
// requirement IDs present in the previous document (first) but not the current
// (second). Grype-converted requirement IDs are the namespaced vulnerability IDs
// (one requirement per match), so deduplication mirrors the raw view's
// distinct-ID semantics.
func HDFDistinctFixedCount(docs [][]byte) (Answer, error) {
	if len(docs) != 2 {
		return Answer{}, fmt.Errorf("temporal diff needs the previous and current documents; got %d", len(docs))
	}
	sets := make([]map[string]bool, 2)
	for i, doc := range docs {
		d, err := parseHDF(doc)
		if err != nil {
			return Answer{}, err
		}
		ids := map[string]bool{}
		for _, r := range hdfRequirements(d) {
			ids[r.ID] = true
		}
		sets[i] = ids
	}
	fixed := 0
	for id := range sets[0] {
		if !sets[1][id] {
			fixed++
		}
	}
	return Answered(strconv.Itoa(fixed)), nil
}

// hdfSystem is the subset of an HDF System document the join reads: components
// and their BOM entries. A BOM may embed the source document or merely reference
// it (ref) — only an embedded document carries the package inventory.
type hdfSystem struct {
	Components []struct {
		Boms []struct {
			Document json.RawMessage `json:"document"`
		} `json:"boms"`
	} `json:"components"`
}

// HDFVulnFreePackageCount is the HDF view of the SBOM×vuln join, over the grype
// results document (first) and the system document (second). It answers ONLY if
// the system document embeds the SBOM's package inventory; the current SPDX
// import carries the BOM as a reference (boms[].ref) with no embedded document,
// so the join key is simply absent from the HDF view and the answer is
// unanswerable — a genuine raw-only (Class D) fact. If a future converter embeds
// the document, this starts answering and the question auto-reclassifies.
func HDFVulnFreePackageCount(docs [][]byte) (Answer, error) {
	if len(docs) != 2 {
		return Answer{}, fmt.Errorf("sbom join needs the results and system documents; got %d", len(docs))
	}
	var sys hdfSystem
	if err := json.Unmarshal(docs[1], &sys); err != nil {
		return Answer{}, fmt.Errorf("parse hdf system: %w", err)
	}
	var packages rawSPDX
	embedded := false
	for _, c := range sys.Components {
		for _, b := range c.Boms {
			if len(b.Document) == 0 || string(b.Document) == "null" {
				continue
			}
			if err := json.Unmarshal(b.Document, &packages); err != nil {
				return Answer{}, fmt.Errorf("parse embedded bom document: %w", err)
			}
			embedded = true
		}
	}
	if !embedded || len(packages.Packages) == 0 {
		return Unanswerable(), nil
	}

	d, err := parseHDF(docs[0])
	if err != nil {
		return Answer{}, err
	}
	vulnerable := map[string]bool{}
	for _, r := range hdfRequirements(d) {
		if r.Code == "" {
			continue
		}
		var m struct {
			Artifact struct {
				Name string `json:"name"`
			} `json:"artifact"`
		}
		if err := json.Unmarshal([]byte(r.Code), &m); err != nil {
			return Answer{}, fmt.Errorf("parse requirement code: %w", err)
		}
		vulnerable[m.Artifact.Name] = true
	}
	free := 0
	for _, p := range packages.Packages {
		if !vulnerable[p.Name] {
			free++
		}
	}
	return Answered(strconv.Itoa(free)), nil
}

// HDFImpactTotalAtLeast is the multi-document HDF view of a cross-format
// aggregate: the impact>=threshold count summed over every converted document.
// After normalization one impact scale spans all source vocabularies, so the
// sum is a single filter repeated — the whole point of the join-point design.
func HDFImpactTotalAtLeast(threshold float64) func([][]byte) (Answer, error) {
	single := HDFImpactCountAtLeast(threshold)
	return func(docs [][]byte) (Answer, error) {
		if len(docs) == 0 {
			return Answer{}, fmt.Errorf("no documents supplied")
		}
		total := 0
		for _, d := range docs {
			a, err := single(d)
			if err != nil {
				return Answer{}, err
			}
			n, err := strconv.Atoi(a.Value)
			if err != nil {
				return Answer{}, err
			}
			total += n
		}
		return Answered(strconv.Itoa(total)), nil
	}
}

// HDFImpactPresentAtLeast reports whether any requirement's impact is at least
// threshold (e.g. a Critical finding maps to impact 0.9).
func HDFImpactPresentAtLeast(threshold float64) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		d, err := parseHDF(b)
		if err != nil {
			return Answer{}, err
		}
		for _, r := range hdfRequirements(d) {
			if r.Impact >= threshold {
				return Answered("true"), nil
			}
		}
		return Answered("false"), nil
	}
}

// lastSegment returns the substring after the final '/', or s if there is none.
func lastSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

// HDFRelatedVulnCountFromCode counts requirements whose retained `code` payload
// lists at least one related vulnerability.
//
// It exists to establish a fact the study needs to state precisely: HDF
// conversion is NOT lossy here. The converter stores the original scanner finding
// byte-for-byte in `code`, so the information is present in the document and the
// question is Class A — both views can answer it. What the HDF *arm* lacks is a
// read tool that projects `code`. Grading this against an answerable key is the
// strict choice on purpose: excusing the HDF arm as out-of-remit would hide a real
// surface limitation behind a claim of information loss that is not true.
func HDFRelatedVulnCountFromCode(b []byte) (Answer, error) {
	var d struct {
		Baselines []struct {
			Requirements []struct {
				Code string `json:"code"`
			} `json:"requirements"`
		} `json:"baselines"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return Answer{}, fmt.Errorf("parse hdf: %w", err)
	}
	n := 0
	for _, bl := range d.Baselines {
		for _, r := range bl.Requirements {
			var finding struct {
				Related []json.RawMessage `json:"relatedVulnerabilities"`
			}
			if err := json.Unmarshal([]byte(r.Code), &finding); err != nil {
				continue // a non-JSON code payload simply carries no related vulns
			}
			if len(finding.Related) > 0 {
				n++
			}
		}
	}
	return Answered(strconv.Itoa(n)), nil
}

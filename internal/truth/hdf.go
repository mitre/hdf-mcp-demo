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

// lastSegment returns the substring after the final '/', or s if there is none.
func lastSegment(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
}

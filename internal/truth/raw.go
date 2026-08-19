package truth

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// rawGosec is the subset of gosec JSON the classifier reads. gosec emits one
// entry per finding site, so an identical rule flagged in N files appears N times
// — the raw count that HDF later deduplicates by rule.
type rawGosec struct {
	Issues []struct {
		RuleID   string `json:"rule_id"`
		Severity string `json:"severity"`
	} `json:"Issues"`
}

func parseGosec(b []byte) (rawGosec, error) {
	var g rawGosec
	if err := json.Unmarshal(b, &g); err != nil {
		return rawGosec{}, fmt.Errorf("parse gosec: %w", err)
	}
	return g, nil
}

// GosecFindingCount is the raw finding total (one per site, pre-dedup).
func GosecFindingCount(b []byte) (Answer, error) {
	g, err := parseGosec(b)
	if err != nil {
		return Answer{}, err
	}
	return Answered(strconv.Itoa(len(g.Issues))), nil
}

// GosecRulePresent reports whether a rule id was flagged. Existence survives
// dedup, so this is answer-preserving against the HDF view.
func GosecRulePresent(ruleID string) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		g, err := parseGosec(b)
		if err != nil {
			return Answer{}, err
		}
		for _, i := range g.Issues {
			if i.RuleID == ruleID {
				return Answered("true"), nil
			}
		}
		return Answered("false"), nil
	}
}

// rawGrype is the subset of grype JSON the classifier reads. Unlike gosec, grype
// emits one match per vulnerable package and does not collapse them, so its raw
// count is preserved through conversion (a useful contrast — divergence is a
// property of the scanner's output shape, not of normalization in general).
type rawGrype struct {
	Matches []struct {
		Vulnerability struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
		} `json:"vulnerability"`
	} `json:"matches"`
}

func parseGrype(b []byte) (rawGrype, error) {
	var g rawGrype
	if err := json.Unmarshal(b, &g); err != nil {
		return rawGrype{}, fmt.Errorf("parse grype: %w", err)
	}
	return g, nil
}

// GrypeMatchCount is the number of vulnerability matches.
func GrypeMatchCount(b []byte) (Answer, error) {
	g, err := parseGrype(b)
	if err != nil {
		return Answer{}, err
	}
	return Answered(strconv.Itoa(len(g.Matches))), nil
}

// GrypeCVEPresent reports whether a CVE id appears among the matches.
func GrypeCVEPresent(cve string) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		g, err := parseGrype(b)
		if err != nil {
			return Answer{}, err
		}
		for _, m := range g.Matches {
			if m.Vulnerability.ID == cve {
				return Answered("true"), nil
			}
		}
		return Answered("false"), nil
	}
}

// GrypeSeverityPresent reports whether any match has the given severity (e.g.
// "Critical"). Existence of a severity band survives normalization.
func GrypeSeverityPresent(sev string) func([]byte) (Answer, error) {
	return func(b []byte) (Answer, error) {
		g, err := parseGrype(b)
		if err != nil {
			return Answer{}, err
		}
		for _, m := range g.Matches {
			if m.Vulnerability.Severity == sev {
				return Answered("true"), nil
			}
		}
		return Answered("false"), nil
	}
}

// rawZap is the subset of ZAP JSON the classifier reads: alerts across sites, each
// with a numeric riskcode (0 informational, 1 low, 2 medium, 3 high). ZAP does not
// deduplicate, so its alert count is preserved through conversion.
type rawZap struct {
	Site []struct {
		Alerts []struct {
			RiskCode string `json:"riskcode"`
		} `json:"alerts"`
	} `json:"site"`
}

func parseZap(b []byte) (rawZap, error) {
	var z rawZap
	if err := json.Unmarshal(b, &z); err != nil {
		return rawZap{}, fmt.Errorf("parse zap: %w", err)
	}
	return z, nil
}

func zapAlerts(z rawZap) []string {
	var out []string
	for _, s := range z.Site {
		for _, a := range s.Alerts {
			out = append(out, a.RiskCode)
		}
	}
	return out
}

// ZapAlertCount is the total number of ZAP alerts across all sites.
func ZapAlertCount(b []byte) (Answer, error) {
	z, err := parseZap(b)
	if err != nil {
		return Answer{}, err
	}
	return Answered(strconv.Itoa(len(zapAlerts(z)))), nil
}

// ZapHighCount is the number of high-risk ZAP alerts (riskcode 3).
func ZapHighCount(b []byte) (Answer, error) {
	z, err := parseZap(b)
	if err != nil {
		return Answer{}, err
	}
	n := 0
	for _, rc := range zapAlerts(z) {
		if rc == "3" {
			n++
		}
	}
	return Answered(strconv.Itoa(n)), nil
}

// rawInspec is the subset of an InSpec ExecJSON run the classifier reads. InSpec
// reports per-control results, and a control may have several (one per test), so
// control count and result count are different questions — the same distinction
// gosec forces between rules and finding sites.
type rawInspec struct {
	Profiles []struct {
		Controls []struct {
			ID      string `json:"id"`
			Results []struct {
				Status string `json:"status"`
			} `json:"results"`
		} `json:"controls"`
	} `json:"profiles"`
}

func parseInspec(b []byte) (rawInspec, error) {
	var i rawInspec
	if err := json.Unmarshal(b, &i); err != nil {
		return rawInspec{}, fmt.Errorf("parse inspec: %w", err)
	}
	return i, nil
}

// InspecControlCount is the number of controls across all profiles. InSpec is
// already rule-shaped, so normalization has nothing to deduplicate and this
// agrees with the HDF requirement count — an apples-to-apples Class-A count over
// a large document.
func InspecControlCount(b []byte) (Answer, error) {
	i, err := parseInspec(b)
	if err != nil {
		return Answer{}, err
	}
	n := 0
	for _, p := range i.Profiles {
		n += len(p.Controls)
	}
	return Answered(strconv.Itoa(n)), nil
}

// InspecComplianceRate is passed results / total results as a whole-number
// percent. Unlike a vulnerability scanner, a compliance run states pass and fail
// natively, so the raw view CAN answer this — it is the fair counterpart to the
// Class-C grype rate, and it must match HDFComplianceRate's result-level
// definition to stay comparable.
func InspecComplianceRate(b []byte) (Answer, error) {
	i, err := parseInspec(b)
	if err != nil {
		return Answer{}, err
	}
	var passed, total int
	for _, p := range i.Profiles {
		for _, c := range p.Controls {
			for _, r := range c.Results {
				total++
				if r.Status == "passed" {
					passed++
				}
			}
		}
	}
	if total == 0 {
		return Answered("0"), nil
	}
	return Answered(strconv.Itoa(passed * 100 / total)), nil
}

// gosecHighCount is the number of gosec findings at severity HIGH — gosec's top
// band (its scale is LOW/MEDIUM/HIGH), so this is its "high or above" count.
func gosecHighCount(b []byte) (int, error) {
	g, err := parseGosec(b)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, i := range g.Issues {
		if i.Severity == "HIGH" {
			n++
		}
	}
	return n, nil
}

// grypeHighPlusCount is the number of grype matches at High or Critical.
func grypeHighPlusCount(b []byte) (int, error) {
	g, err := parseGrype(b)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range g.Matches {
		if s := m.Vulnerability.Severity; s == "High" || s == "Critical" {
			n++
		}
	}
	return n, nil
}

// CrossFormatHighSeverityCount is the raw view of the cross-format aggregate:
// the total high-or-above findings across three UNLIKE severity vocabularies —
// gosec (HIGH on a LOW/MEDIUM/HIGH scale), ZAP (riskcode 3 on 0–3), and grype
// (High/Critical on Critical..Unknown). The raw arm must reconcile all three
// vocabularies itself; the sources arrive in declaration order (gosec, zap,
// grype). This is the normalization-as-join-point case HDF exists for.
func CrossFormatHighSeverityCount(docs [][]byte) (Answer, error) {
	if len(docs) != 3 {
		return Answer{}, fmt.Errorf("cross-format count needs gosec, zap, grype documents; got %d", len(docs))
	}
	gosecN, err := gosecHighCount(docs[0])
	if err != nil {
		return Answer{}, err
	}
	zapA, err := ZapHighCount(docs[1])
	if err != nil {
		return Answer{}, err
	}
	zapN, err := strconv.Atoi(zapA.Value)
	if err != nil {
		return Answer{}, err
	}
	grypeN, err := grypeHighPlusCount(docs[2])
	if err != nil {
		return Answer{}, err
	}
	return Answered(strconv.Itoa(gosecN + zapN + grypeN)), nil
}

// GrypeRelatedVulnCount counts matches carrying at least one related
// vulnerability. This is a tool-specific field: grype records it, and while HDF
// conversion preserves it verbatim inside the requirement's `code` payload, no
// HDF read tool projects `code` — so it is the category-5 case where the bounded
// surface, not normalization, is what costs the HDF arm.
func GrypeRelatedVulnCount(b []byte) (Answer, error) {
	var g struct {
		Matches []struct {
			Related []json.RawMessage `json:"relatedVulnerabilities"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(b, &g); err != nil {
		return Answer{}, fmt.Errorf("parse grype: %w", err)
	}
	n := 0
	for _, m := range g.Matches {
		if len(m.Related) > 0 {
			n++
		}
	}
	return Answered(strconv.Itoa(n)), nil
}

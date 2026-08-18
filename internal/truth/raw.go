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

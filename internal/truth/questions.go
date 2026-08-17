package truth

// Candidate is one question in the bank plus which fixture it reads. The
// classifier needs the raw fixture and its converted HDF document to compute the
// two ground-truth views; Fixture/HDF name those files so a harness can supply
// the right bytes.
type Candidate struct {
	Question
	Fixture string // raw fixture filename (under fixtures/)
	From    string // hdf_convert source format for producing the HDF view
	HDFName string // conventional converted filename
}

// Bank returns the candidate questions the classifier tags. It is deliberately
// seeded to exercise all three classes from a single small scanner (gosec) so the
// core taxonomy is provable offline, plus grype questions that show divergence is
// a property of the scanner's output shape — grype does not deduplicate, so its
// count is answer-preserving where gosec's is not.
func Bank() []Candidate {
	return []Candidate{
		{
			Question: Question{
				ID:     "gosec-finding-count",
				Prompt: "How many findings are in the gosec (SAST) scan?",
				Raw:    GosecFindingCount,
				HDF:    HDFRequirementCount,
			},
			Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json",
		},
		{
			Question: Question{
				ID:     "gosec-rule-present",
				Prompt: "Is rule G304 flagged in the gosec (SAST) scan?",
				Raw:    GosecRulePresent("G304"),
				HDF:    HDFRequirementPresent("G304"),
			},
			Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json",
		},
		{
			Question: Question{
				ID:     "gosec-compliance-rate",
				Prompt: "What is the compliance pass rate of the gosec (SAST) scan?",
				Raw:    AlwaysUnanswerable,
				HDF:    HDFComplianceRate,
			},
			Fixture: "gosec.json", From: "gosec", HDFName: "gosec.hdf.json",
		},
		{
			Question: Question{
				ID:     "grype-match-count",
				Prompt: "How many vulnerability matches are in the grype scan?",
				Raw:    GrypeMatchCount,
				HDF:    HDFRequirementCount,
			},
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
		},
		{
			Question: Question{
				ID:     "grype-cve-present",
				Prompt: "Is CVE-2021-36159 present in the grype scan?",
				Raw:    GrypeCVEPresent("CVE-2021-36159"),
				HDF:    HDFRequirementPresent("CVE-2021-36159"),
			},
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
		},
		{
			Question: Question{
				ID:     "grype-compliance-rate",
				Prompt: "What is the compliance pass rate of the grype scan?",
				Raw:    AlwaysUnanswerable,
				HDF:    HDFComplianceRate,
			},
			Fixture: "grype.json", From: "grype", HDFName: "grype.hdf.json",
		},
	}
}

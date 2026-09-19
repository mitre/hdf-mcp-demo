# Benchmark questions

This file is the wording of every question the benchmark asks. It is the
human-written half of the study, so it lives here as text rather than in Go:
edit it, or copy it and pass your copy with `-questions FILE`, to change what
the models are asked without touching code.

### How it works

Every second-level `## heading` names a question by its ID, so that level is
reserved: prose sections like this one use `###`. The paragraph under a
question heading is the prompt sent to both arms. Blank lines and line wrapping
inside a prompt are collapsed to single spaces; HTML comments are ignored.

The ID binds the prompt to its ground truth, which is computed from the data by
`internal/benchmark/questions.go` and never read from this file. Three rules
follow from that:

- **Keep the IDs.** An unknown ID is an error, because there is no answer key
  for it. Rewording is free; asking a different fact under the same ID grades
  your new prompt against the old answer.
- **A copy you pass with `-questions` defines the run.** Only the IDs present
  in it run, in the order written, so deleting a heading drops that question.
- **Do not name the files.** The harness appends the file clause per arm ("The
  scan file is named grype.json." / "The HDF document is named grype.hdf.json."),
  and the system prompt asks for the final `ANSWER: <value>` line. Where a
  prompt says FIRST and SECOND it refers to that appended order.

The sections below are the vetted default set. `README.md` explains why each
question is in the bank and how it is graded.

## gosec-distinct-rules

How many DISTINCT rule violations (unique rule IDs) are in the gosec SAST scan?

## gosec-total-findings

How many total findings did the gosec SAST scanner emit (the raw count of
individual finding entries, before any de-duplication)?

## grype-match-count

How many vulnerability matches are in the grype scan?

## grype-cve-present

Is CVE-2021-36159 present in the grype scan? Answer yes or no.

## grype-compliance-rate

What percentage of the grype scan is passing (the compliance pass rate)? Answer
with a whole-number percentage.

## grype-has-critical

Does the grype scan contain any Critical-severity finding? Answer yes or no.

## grype-related-vulns

How many vulnerability matches in the grype scan list at least one related
vulnerability?

## zap-alert-count

How many alerts are in the ZAP (DAST) scan?

## inspec-control-count

How many controls are in the InSpec compliance run?

## inspec-compliance-rate

What percentage of the InSpec compliance run is passing (the pass rate over all
test results)? Answer with a whole-number percentage.

## zap-high-severity-count

How many high-risk alerts are in the ZAP scan?

## cross-format-high-count

Across the gosec (SAST), ZAP (DAST), and grype (vulnerability) scans together,
how many findings are high severity or above?

## grype-fixed-vulns

Two grype scans of the same container image are provided: the FIRST named file
is the previous scan and the SECOND is the current scan. How many DISTINCT
vulnerability IDs from the previous scan are no longer present in the current
scan?

## sbom-vuln-free-packages

An SPDX SBOM inventories every package of the same container image the grype
scan covers: the FIRST named file is the vulnerability scan and the SECOND is
the SBOM. How many of the SBOM's packages have NO vulnerability matches in the
grype scan?

## multi-distinct-cwe-count

How many DISTINCT CWE IDs are referenced across the gosec (SAST), ZAP (DAST),
and grype (vulnerability) scans together? Count a weakness once no matter how
many findings cite it.

## multi-zap-high-count

Considering only the ZAP (DAST) scan, how many of its findings are high
severity or above?

## multi-nist-sc-failed-count

Across the gosec (SAST), ZAP (DAST), and grype (vulnerability) scans together,
how many failed requirements map to the NIST 800-53 SC (System and
Communications Protection) control family?

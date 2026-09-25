// Package hdfmcpdemo is the module root. It exists to embed the files that are
// kept at the top of the repository because people edit them — the benchmark's
// question prompts — so a binary carries them without a runtime path lookup.
package hdfmcpdemo

import _ "embed"

// QuestionsMD is questions.md: the vetted wording of every benchmark question,
// keyed by ID. internal/benchmark binds it to the ground-truth skeleton.
//
//go:embed questions.md
var QuestionsMD string

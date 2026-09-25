package instrument

import (
	"os"
	"strings"
)

// SplitList parses a comma-separated list the way every list-valued input to
// the study is parsed — the -models and -tools flags and OPENAI_MODEL_LIST —
// trimming each item and dropping blanks, so "a, b ,," yields ["a","b"] and a
// value with no items yields nil. It is the one definition; do not fork it.
func SplitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// ModelsFromEnv returns the models a live run should exercise: OPENAI_MODEL_LIST
// (comma-delimited) when it names at least one model, else the singleton
// OPENAI_MODEL, else nil. Live tests loop over this (one sub-test per model), so
// a single invocation can cover several models. It lives here rather than in
// cmd/benchmark because the live tests of three packages share it.
func ModelsFromEnv() []string {
	if list := SplitList(os.Getenv("OPENAI_MODEL_LIST")); len(list) > 0 {
		return list
	}
	if m := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); m != "" {
		return []string{m}
	}
	return nil
}

package instrument

import (
	"os"
	"strings"
)

// ModelsFromEnv returns the models a live run should exercise: OPENAI_MODEL_LIST
// (comma-delimited) if set, else the singleton OPENAI_MODEL, else empty. Blanks
// are trimmed and dropped, so "a, b ,," yields ["a","b"]. Live tests loop over
// this (one sub-test per model), so a single invocation can cover several models.
func ModelsFromEnv() []string {
	if list := strings.TrimSpace(os.Getenv("OPENAI_MODEL_LIST")); list != "" {
		var out []string
		for _, m := range strings.Split(list, ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, m)
			}
		}
		return out
	}
	if m := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); m != "" {
		return []string{m}
	}
	return nil
}

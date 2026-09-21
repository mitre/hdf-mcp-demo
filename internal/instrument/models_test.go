package instrument

import (
	"reflect"
	"testing"
)

// TestSplitList pins the one comma-list parser the module uses for -models,
// -tools and OPENAI_MODEL_LIST: split on commas, trim each item, drop blanks,
// and yield nil (not an empty slice) for nothing, so callers can test len().
func TestSplitList(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"a, b ,,", []string{"a", "b"}},
		{"gemma-4,gpt-oss-120b", []string{"gemma-4", "gpt-oss-120b"}},
		{"  hdf_open , hdf_query ", []string{"hdf_open", "hdf_query"}},
		{"", nil},
		{" , , ", nil},
		{"single", []string{"single"}},
		{"a b, c", []string{"a b", "c"}},  // interior spaces are part of the item
		{"\ta\t,\tb", []string{"a", "b"}}, // tabs trim like spaces
	} {
		if got := SplitList(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitList(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

// TestModelsFromEnv pins the documented resolution order for the first time:
// OPENAI_MODEL_LIST wins when set, OPENAI_MODEL is the singleton fallback, and
// blanks in either are dropped rather than becoming empty model names.
func TestModelsFromEnv(t *testing.T) {
	for _, tc := range []struct {
		name, list, single string
		want               []string
	}{
		{"list wins over single", "a, b ,,", "c", []string{"a", "b"}},
		{"single alone", "", " c ", []string{"c"}},
		{"blank list falls back to single", " , ", "c", []string{"c"}},
		{"neither set", "", "", nil},
		{"blank single", "", "  ", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENAI_MODEL_LIST", tc.list)
			t.Setenv("OPENAI_MODEL", tc.single)
			if got := ModelsFromEnv(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ModelsFromEnv() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

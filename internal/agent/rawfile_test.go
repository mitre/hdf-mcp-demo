package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func rawArgs(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRawFileToolBox_ReadFileGuard(t *testing.T) {
	dir := t.TempDir()
	small := "small file contents"
	big := strings.Repeat("x", 40*1024)
	if err := os.WriteFile(filepath.Join(dir, "small.txt"), []byte(small), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)

	got, err := tb.Execute(context.Background(), "read_file", rawArgs(t, map[string]any{"name": "small.txt"}))
	if err != nil || got != small {
		t.Errorf("read_file(small) = %q, err %v; want the contents", got, err)
	}

	got, err = tb.Execute(context.Background(), "read_file", rawArgs(t, map[string]any{"name": "big.json"}))
	if err != nil {
		t.Fatalf("read_file(big) errored: %v", err)
	}
	if !strings.Contains(got, "too large") || !strings.Contains(got, "search_file") {
		t.Errorf("read_file(big) should refuse and point to search_file/read_lines, got: %q", got)
	}
}

func TestRawFileToolBox_Search(t *testing.T) {
	dir := t.TempDir()
	content := "alpha\nbravo CVE-1\ncharlie\ndelta CVE-1\nCVE-1 echo\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)

	got, err := tb.Execute(context.Background(), "search_file", rawArgs(t, map[string]any{"name": "f.txt", "query": "CVE-1"}))
	if err != nil {
		t.Fatal(err)
	}
	// Counts are MATCHES now, not matching lines — the distinction is invisible
	// here (one hit per line) and decisive on a minified document.
	if !strings.HasPrefix(got, "3 matches") {
		t.Errorf("search should count 3 matches, got: %q", got)
	}
	if !strings.Contains(got, "L2:") || !strings.Contains(got, "L5:") {
		t.Errorf("search should report line numbers, got: %q", got)
	}

	got, _ = tb.Execute(context.Background(), "search_file", rawArgs(t, map[string]any{"name": "f.txt", "query": "zzz"}))
	if !strings.HasPrefix(got, "0 matches") {
		t.Errorf("no-match search should report 0, got: %q", got)
	}
}

func TestRawFileToolBox_ReadLines(t *testing.T) {
	dir := t.TempDir()
	content := "one\ntwo\nthree\nfour\nfive\n"
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)

	got, err := tb.Execute(context.Background(), "read_lines", rawArgs(t, map[string]any{"name": "f.txt", "offset": 1, "limit": 2}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "L2: two") || !strings.Contains(got, "L3: three") {
		t.Errorf("read_lines(offset=1,limit=2) should return lines 2-3, got: %q", got)
	}
	if strings.Contains(got, "one") || strings.Contains(got, "four") {
		t.Errorf("read_lines window leaked neighbors: %q", got)
	}

	got, _ = tb.Execute(context.Background(), "read_lines", rawArgs(t, map[string]any{"name": "f.txt", "offset": 99}))
	if !strings.Contains(got, "past end") {
		t.Errorf("out-of-range offset should say past end, got: %q", got)
	}
}

func TestRawFileToolBox_Errors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)
	if _, err := tb.Execute(context.Background(), "read_file", rawArgs(t, map[string]any{})); err == nil {
		t.Error("missing name should error")
	}
	if _, err := tb.Execute(context.Background(), "read_file", rawArgs(t, map[string]any{"name": "missing.txt"})); err == nil {
		t.Error("missing file should error")
	}
	if _, err := tb.Execute(context.Background(), "bogus", rawArgs(t, map[string]any{"name": "x.txt"})); err == nil {
		t.Error("unknown tool should error")
	}
}

// minified is the shape every study fixture but gosec.json actually has: one
// long line. The old 300-character clip made these effectively unreadable, and
// line-based counting made "how many findings" unanswerable on them.
const minified = `{"vulnerabilities":[` +
	`{"id":"A","severity":"high"},{"id":"B","severity":"high"},` +
	`{"id":"C","severity":"critical"},{"id":"D","severity":"low"}]}`

// TestRawFile_RegexSearchAndUntruncatedMatches is the card's first failing test.
// A competent agent has grep with a regex; expressing a field-and-value pattern
// must not depend on guessing exact JSON spacing.
func TestRawFile_RegexSearchAndUntruncatedMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.json"), []byte(minified), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)

	// A regex tolerant of whitespace — the thing a substring cannot express.
	out, err := tb.Execute(context.Background(), "search_file",
		json.RawMessage(`{"name":"scan.json","query":"\"severity\"\\s*:\\s*\"high\"","regex":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("regex search should find 2 high-severity matches, got:\n%s", out)
	}
	// The whole document is one line; clipping it at 300 chars would hide matches
	// the model cannot know it is missing.
	if strings.Contains(out, "…") && !strings.Contains(out, "match") {
		t.Errorf("matches on a single-line document must not be silently clipped:\n%s", out)
	}
}

// TestRawFile_CountsMatchesNotLines pins the error that produced a wrong probe
// result: on a minified document every finding is on the SAME line, so counting
// lines answers 1 where the true count is 3.
func TestRawFile_CountsMatchesNotLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.json"), []byte(minified), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)
	out, err := tb.Execute(context.Background(), "search_file",
		json.RawMessage(`{"name":"scan.json","query":"\"severity\":","regex":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "4") {
		t.Errorf("should report 4 matches (not 1 matching line) for a minified document:\n%s", out)
	}
}

// TestRawFile_JSONSelect gives the arm the structured extraction a real agent
// gets from jq. Without it, counting entries in a JSON array is only reachable
// by text heuristics, which is what made the arm miscount findings as lines.
func TestRawFile_JSONSelect(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "scan.json"), []byte(minified), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)
	out, err := tb.Execute(context.Background(), "json_select",
		json.RawMessage(`{"name":"scan.json","path":"vulnerabilities"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "4") {
		t.Errorf("json_select on the array should report its 4 elements:\n%s", out)
	}
}

// TestRawFile_StaysBounded guards the anti-pattern: strengthening the arm must
// not turn it into "return the whole file", which would rebuild the whole-file
// strawman the ingest-ceiling framing had to be corrected for.
func TestRawFile_StaysBounded(t *testing.T) {
	dir := t.TempDir()
	big := `{"items":[` + strings.Repeat(`{"severity":"high","note":"`+strings.Repeat("x", 400)+`"},`, 500) + `{}]}`
	if err := os.WriteFile(filepath.Join(dir, "big.json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)
	for _, call := range []struct{ tool, args string }{
		{"search_file", `{"name":"big.json","query":"severity"}`},
		{"json_select", `{"name":"big.json","path":"items"}`},
	} {
		out, err := tb.Execute(context.Background(), call.tool, json.RawMessage(call.args))
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > 24000 {
			t.Errorf("%s returned %d bytes — responses must stay bounded", call.tool, len(out))
		}
	}
}

// TestRawFile_ResponsesStayLean pins the sizing lesson: a tool response is
// re-sent on every later turn, so verbosity compounds. Generous windows and
// previews raised the raw arm's cost 74% and made it hit the iteration cap MORE
// often — the capability was right, the volume was not.
func TestRawFile_ResponsesStayLean(t *testing.T) {
	dir := t.TempDir()
	big := `{"items":[` + strings.Repeat(`{"severity":"high","note":"`+strings.Repeat("x", 400)+`"},`, 500) + `{}]}`
	if err := os.WriteFile(filepath.Join(dir, "big.json"), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	tb := NewRawFileToolBox(dir)
	for _, call := range []struct {
		tool, args string
		max        int
	}{
		{"search_file", `{"name":"big.json","query":"severity"}`, 3500},
		{"json_select", `{"name":"big.json","path":"items"}`, 800},
	} {
		out, err := tb.Execute(context.Background(), call.tool, json.RawMessage(call.args))
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > call.max {
			t.Errorf("%s returned %d bytes, want <= %d — responses compound across turns", call.tool, len(out), call.max)
		}
		// The count must survive the trimming: it is the answer-bearing part.
		if call.tool == "search_file" && !strings.Contains(out, "500 matches") {
			t.Errorf("search_file must still report the complete match count:\n%s", out)
		}
	}
}

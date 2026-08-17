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
	if !strings.HasPrefix(got, "3 lines contain") {
		t.Errorf("search should count 3 matches, got: %q", got)
	}
	if !strings.Contains(got, "L2:") || !strings.Contains(got, "L5:") {
		t.Errorf("search should report line numbers, got: %q", got)
	}

	got, _ = tb.Execute(context.Background(), "search_file", rawArgs(t, map[string]any{"name": "f.txt", "query": "zzz"}))
	if !strings.HasPrefix(got, "0 lines contain") {
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

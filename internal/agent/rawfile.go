package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
)

// defaultReadFileMaxBytes bounds how much read_file will return whole. Above it,
// read_file refuses and points the model at search_file/read_lines — the honest
// stand-in for a real agent hitting its context limit on a big scan (a 156k-token
// grype scan does not fit, so "just read the file" is not an available strategy).
const defaultReadFileMaxBytes = 32 * 1024

// RawFileToolBox is the raw-file arm's tool surface. It deliberately gives the
// model NO format knowledge: a size-guarded read_file, a line grep (search_file),
// and a paginated reader (read_lines). To answer a question the model must
// discover the file's structure itself and choose what to search for — the real
// cost of working a raw scan without HDF's normalized schema. (Line-oriented, so
// it assumes human-readable/pretty-printed input; a minified single-line file
// would defeat the line tools — a limitation to revisit with the fuller baseline.)
type RawFileToolBox struct {
	baseDir  string
	maxBytes int
}

// NewRawFileToolBox confines reads to baseDir.
func NewRawFileToolBox(baseDir string) *RawFileToolBox {
	return &RawFileToolBox{baseDir: baseDir, maxBytes: defaultReadFileMaxBytes}
}

// Definitions advertises the three format-agnostic file tools.
func (b *RawFileToolBox) Definitions() []instrument.Tool {
	strProp := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	intProp := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	return []instrument.Tool{
		{
			Name:        "read_file",
			Description: "Return a file's full contents. Fails for large files; use search_file or read_lines instead.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": strProp("the file name, e.g. scan.json")},
				"required":   []string{"name"},
			},
		},
		{
			Name:        "search_file",
			Description: "Search a file for lines containing a substring. Returns the total match count and the first matching lines (with line numbers).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        strProp("the file name"),
					"query":       strProp("substring to search for"),
					"max_results": intProp("max matching lines to return (default 20)"),
				},
				"required": []string{"name", "query"},
			},
		},
		{
			Name:        "read_lines",
			Description: "Return a window of lines from a file (with line numbers) plus the total line count, for paging through it.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":   strProp("the file name"),
					"offset": intProp("0-based first line to return (default 0)"),
					"limit":  intProp("number of lines to return (default 40)"),
				},
				"required": []string{"name"},
			},
		},
	}
}

// Execute routes a raw-file tool call.
func (b *RawFileToolBox) Execute(_ context.Context, name string, args json.RawMessage) (string, error) {
	var a struct {
		Name       string `json:"name"`
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
	}
	if a.Name == "" {
		return "", fmt.Errorf("missing required argument: name")
	}
	data, err := b.read(a.Name)
	if err != nil {
		return "", err
	}

	switch name {
	case "read_file":
		if len(data) > b.maxBytes {
			return fmt.Sprintf("file %q is too large to read whole (%d bytes, ~%d tokens; limit %d bytes). "+
				"Use search_file to grep for a substring, or read_lines to page through it.",
				filepath.Base(a.Name), len(data), len(data)/4, b.maxBytes), nil
		}
		return string(data), nil
	case "search_file":
		return searchFile(string(data), a.Query, a.MaxResults), nil
	case "read_lines":
		return readLines(string(data), a.Offset, a.Limit), nil
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// read returns the confined file's bytes (base name only — no traversal).
func (b *RawFileToolBox) read(name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(b.baseDir, filepath.Base(name)))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", filepath.Base(name), err)
	}
	return data, nil
}

const maxLineLen = 300 // truncate long lines so a match/window stays bounded

func searchFile(content, query string, maxResults int) string {
	if query == "" {
		return "search_file requires a non-empty query"
	}
	if maxResults <= 0 {
		maxResults = 20
	} else if maxResults > 100 {
		maxResults = 100
	}
	var hits []string
	total := 0
	for i, line := range strings.Split(content, "\n") {
		if strings.Contains(line, query) {
			total++
			if len(hits) < maxResults {
				hits = append(hits, fmt.Sprintf("L%d: %s", i+1, clip(strings.TrimSpace(line))))
			}
		}
	}
	if total == 0 {
		return fmt.Sprintf("0 lines contain %q", query)
	}
	shown := len(hits)
	return fmt.Sprintf("%d lines contain %q (showing first %d):\n%s", total, query, shown, strings.Join(hits, "\n"))
}

func readLines(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 40
	} else if limit > 200 {
		limit = 200
	}
	if offset >= len(lines) {
		return fmt.Sprintf("offset %d is past end of file (%d lines)", offset, len(lines))
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "lines %d-%d of %d:\n", offset, end-1, len(lines))
	for i := offset; i < end; i++ {
		fmt.Fprintf(&b, "L%d: %s\n", i+1, clip(lines[i]))
	}
	return b.String()
}

func clip(s string) string {
	if len(s) <= maxLineLen {
		return s
	}
	return s[:maxLineLen] + "…"
}

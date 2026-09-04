package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
			Name: "search_file",
			Description: "Search a file for a substring or regular expression. Returns the TOTAL number of matches " +
				"(not matching lines — a minified file puts every match on one line) and a window of text around each hit.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":        strProp("the file name"),
					"query":       strProp("substring, or a regular expression when regex is true"),
					"regex":       map[string]any{"type": "boolean", "description": "treat query as a regular expression"},
					"max_results": intProp("max matches to show (default 20)"),
				},
				"required": []string{"name", "query"},
			},
		},
		{
			Name: "json_select",
			Description: "Read a value out of a JSON file by dot path (e.g. \"vulnerabilities\" or \"runs.0.results\"). " +
				"Reports the type, the element count for arrays and objects, and a bounded preview — the reliable way to count entries.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": strProp("the file name"),
					"path": strProp("dot path; empty or \".\" selects the document root"),
				},
				"required": []string{"name"},
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
		Regex      bool   `json:"regex"`
		Path       string `json:"path"`
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
		return searchFile(string(data), a.Query, a.MaxResults, a.Regex)
	case "json_select":
		return jsonSelect(data, a.Path), nil
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

// matchWindow is how much text is returned around each hit. It replaces the old
// fixed 300-character line clip, which silently truncated every study fixture
// but one — nessus.xml's longest line is 893,248 characters, so a line-based
// view of it showed the model 0.03% of the line and gave no hint of the rest.
const matchWindow = 48

// maxSearchBytes bounds the whole response. Strengthening this arm must not turn
// it into "return the file", which would rebuild the whole-file strawman the
// study exists to avoid.
// maxSearchBytes bounds the whole response. It is deliberately tight: a tool
// response is re-sent on every subsequent turn, so verbosity compounds. An
// earlier 20,000-byte cap with 120-character windows raised the raw arm's token
// cost 74% and pushed it into the iteration cap more often — richer tools made
// the agent worse. The COUNT is the valuable part of a search; the windows are
// only there to confirm the pattern matched what the caller meant.
const maxSearchBytes = 3000

// searchFile counts MATCHES, not matching lines, and returns a window of text
// around each. Counting lines answered 1 where a minified document held four
// findings; counting matches is what "how many findings" actually needs.
func searchFile(content, query string, maxResults int, useRegex bool) (string, error) {
	if query == "" {
		return "search_file requires a non-empty query", nil
	}
	if maxResults <= 0 {
		maxResults = 5
	} else if maxResults > 50 {
		maxResults = 50
	}

	var re *regexp.Regexp
	var err error
	if useRegex {
		if re, err = regexp.Compile(query); err != nil {
			return "", fmt.Errorf("invalid regular expression %q: %w", query, err)
		}
	} else {
		re = regexp.MustCompile(regexp.QuoteMeta(query))
	}

	locs := re.FindAllStringIndex(content, -1)
	if len(locs) == 0 {
		return fmt.Sprintf("0 matches for %q", query), nil
	}

	// Line numbers stay useful for line-structured files; they are computed from
	// the match offset so they remain correct on minified input too.
	lineOf := func(off int) int { return strings.Count(content[:off], "\n") + 1 }

	var b strings.Builder
	shown := 0
	fmt.Fprintf(&b, "%d matches for %q (showing first %d):\n", len(locs), query, min(len(locs), maxResults))
	for _, loc := range locs {
		if shown >= maxResults || b.Len() > maxSearchBytes {
			break
		}
		// Clamp the window to the matched line, so a line-structured file reads
		// like grep. Only when the line itself is longer than the window (the
		// minified case, where a "line" can be hundreds of thousands of
		// characters) does the window become the limit.
		lineLo := strings.LastIndexByte(content[:loc[0]], '\n') + 1
		lineHi := loc[1]
		if nl := strings.IndexByte(content[loc[1]:], '\n'); nl >= 0 {
			lineHi = loc[1] + nl
		} else {
			lineHi = len(content)
		}
		lo := max(lineLo, loc[0]-matchWindow)
		hi := min(lineHi, loc[1]+matchWindow)
		snippet := strings.ReplaceAll(strings.TrimSpace(content[lo:hi]), "\n", " ")
		prefix, suffix := "", ""
		if lo > lineLo {
			prefix = "…"
		}
		if hi < lineHi {
			suffix = "…"
		}
		fmt.Fprintf(&b, "L%d: %s%s%s\n", lineOf(loc[0]), prefix, snippet, suffix)
		shown++
	}
	if len(locs) > shown {
		fmt.Fprintf(&b, "(%d further matches not shown; the count above is complete)\n", len(locs)-shown)
	}
	return b.String(), nil
}

// jsonSelect reads a value by dot path and reports its type and size. It is the
// structured extraction a real agent gets from jq: counting the elements of an
// array is exact here, where text heuristics miscount.
func jsonSelect(data []byte, path string) string {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Sprintf("not valid JSON: %v", err)
	}
	cur := doc
	if p := strings.Trim(path, ". "); p != "" {
		for _, seg := range strings.Split(p, ".") {
			switch node := cur.(type) {
			case map[string]any:
				v, ok := node[seg]
				if !ok {
					return fmt.Sprintf("path %q: no key %q (available: %s)", path, seg, strings.Join(keysOf(node), ", "))
				}
				cur = v
			case []any:
				i, err := strconv.Atoi(seg)
				if err != nil || i < 0 || i >= len(node) {
					return fmt.Sprintf("path %q: %q is not a valid index into an array of %d", path, seg, len(node))
				}
				cur = node[i]
			default:
				return fmt.Sprintf("path %q: cannot descend into %T", path, cur)
			}
		}
	}

	switch node := cur.(type) {
	case []any:
		return fmt.Sprintf("array with %d elements at %q\n%s", len(node), pathLabel(path), preview(node))
	case map[string]any:
		return fmt.Sprintf("object with %d keys at %q: %s\n%s",
			len(node), pathLabel(path), strings.Join(keysOf(node), ", "), preview(node))
	default:
		return fmt.Sprintf("%T at %q: %s", cur, pathLabel(path), clipTo(fmt.Sprint(cur), 200))
	}
}

func pathLabel(p string) string {
	if strings.Trim(p, ". ") == "" {
		return "."
	}
	return p
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	if len(out) > 20 {
		out = append(out[:20], "…")
	}
	return out
}

// preview renders a bounded sample so a caller can see the shape without
// receiving the document.
func preview(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return "preview: " + clipTo(string(b), 300)
}

func clipTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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

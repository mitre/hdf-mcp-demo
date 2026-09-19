package benchmark

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	hdfmcpdemo "github.com/mitre/hdf-mcp-demo"
)

// DefaultQuestionsFile is where the built-in prompts live: at the top of the
// repository, because it is the human-written half of the study and people
// edit it. The binary embeds it (hdfmcpdemo.QuestionsMD); Bank binds it to the
// skeleton, and a run can substitute its own copy with LoadBank.
const DefaultQuestionsFile = "questions.md"

// Prompt is one question's wording as read from a questions file: the ID that
// binds it to its ground truth, and the text both arms are asked.
type Prompt struct {
	ID  string
	Ask string
}

var htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// ParsePrompts reads a questions file. Each `## heading` names a question by
// ID; the non-blank lines under it, up to the next heading, are its prompt,
// joined with single spaces so the text can be wrapped. Text before the first
// heading, other heading levels, and HTML comments are ignored. It is an error
// for a heading to repeat, to have no prompt, or for the file to have no
// questions at all — a file that parses to nothing would run nothing and
// report a study with no rows.
func ParsePrompts(r io.Reader) ([]Prompt, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := htmlCommentRE.ReplaceAllString(string(raw), "")

	var (
		out   []Prompt
		seen  = map[string]bool{}
		cur   *Prompt
		lines []string
	)
	flush := func() error {
		if cur == nil {
			return nil
		}
		if len(lines) == 0 {
			return fmt.Errorf("question %q has no prompt text under its heading", cur.ID)
		}
		cur.Ask = strings.Join(lines, " ")
		out = append(out, *cur)
		cur, lines = nil, nil
		return nil
	}

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "##", strings.HasPrefix(line, "## "):
			if err := flush(); err != nil {
				return nil, err
			}
			id := strings.Trim(strings.TrimPrefix(line, "##"), " `")
			if id == "" {
				return nil, fmt.Errorf("a question heading has no ID")
			}
			if seen[id] {
				return nil, fmt.Errorf("question %q appears more than once", id)
			}
			seen[id] = true
			cur = &Prompt{ID: id}
		case line == "", strings.HasPrefix(line, "#"):
			// blank, or a heading at another level: structure, not prompt text
		case cur != nil:
			lines = append(lines, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no questions found: expected `## <question-id>` headings, each followed by its prompt")
	}
	return out, nil
}

// BankFrom binds prompts to the vetted skeleton by ID. The result runs in the
// prompts' order and contains only the IDs the prompts name, so a file is the
// whole definition of a run's question set. An ID the skeleton does not know
// has no ground truth and is refused, naming the IDs that exist.
func BankFrom(prompts []Prompt) ([]Question, error) {
	byID := map[string]Question{}
	for _, q := range skeleton() {
		byID[q.ID] = q
	}
	out := make([]Question, 0, len(prompts))
	for _, p := range prompts {
		q, ok := byID[p.ID]
		if !ok {
			return nil, fmt.Errorf("unknown question %q: prompts bind to ground truth by ID, and only these exist: %s",
				p.ID, strings.Join(knownIDs(byID), ", "))
		}
		q.Ask = p.Ask
		out = append(out, q)
	}
	return out, nil
}

func knownIDs(byID map[string]Question) []string {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LoadBank reads a questions file from disk and binds it to the skeleton.
func LoadBank(path string) ([]Question, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open -questions file: %w", err)
	}
	defer func() { _ = f.Close() }()
	prompts, err := ParsePrompts(f)
	if err != nil {
		return nil, fmt.Errorf("parse -questions file %s: %w", path, err)
	}
	bank, err := BankFrom(prompts)
	if err != nil {
		return nil, fmt.Errorf("-questions file %s: %w", path, err)
	}
	return bank, nil
}

// Bank is the vetted question set with its default wording: the skeleton bound
// to the embedded questions.md. The embedded file is compile-time data pinned
// by test, so a parse failure here is a build defect, not a runtime condition.
func Bank() []Question {
	prompts, err := ParsePrompts(strings.NewReader(hdfmcpdemo.QuestionsMD))
	if err != nil {
		panic("benchmark: embedded questions.md: " + err.Error())
	}
	bank, err := BankFrom(prompts)
	if err != nil {
		panic("benchmark: embedded questions.md: " + err.Error())
	}
	return bank
}

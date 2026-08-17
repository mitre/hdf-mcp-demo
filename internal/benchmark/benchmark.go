// Package benchmark is the graded, model-driven two-arm study (ADR-0002 Phase
// 3d). For each vetted question it computes the fair ground truth per data view
// (via the truth classifier), runs the same model through the raw-file arm and
// the HDF-MCP arm (agent loop), grades each answer against the class-appropriate
// key, and reports per-class accuracy alongside real token cost in two workflow
// views: amortized/pipeline (conversion done out-of-band) and, optionally,
// conversion-included/ad-hoc (the agent converts on demand).
//
// It is deliberately honest: the HDF arm's prompt tokens already include the
// tool-schema tax (the tool definitions ride in every request), grading favors
// abstention over false credit, Class B is scored bidirectionally, and an arm
// that errors or times out (e.g. a raw scan too large to fit context) is recorded
// as a `failed` outcome rather than crashing the run.
//
// Questions run concurrently within a model (bounded by Options.Concurrency); a
// shared MCP session is safe because mcpclient.Session serializes tool calls.
// Models are still iterated serially by the caller — concurrent model *loads*
// thrash a memory-constrained gateway, unlike concurrent questions against one
// already-loaded model.
package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mitre/hdf-mcp-demo/internal/agent"
	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
	"github.com/mitre/hdf-mcp-demo/internal/truth"
)

// DefaultSystem asks for a single parseable final line so grading stays robust
// without an LLM judge.
const DefaultSystem = "You are a security analyst. Use the available tools to get the data you need to answer the question. " +
	"Then give a final line formatted EXACTLY as: ANSWER: <value> — where <value> is a single number, or a single yes/no, " +
	"and nothing else on that line."

// Options controls a run.
type Options struct {
	MaxIters    int    // tool round-trip cap per arm (default 6)
	System      string // system prompt (default DefaultSystem)
	AdHoc       bool   // also measure the conversion-included HDF cost view
	Concurrency int    // max questions in flight at once (default 1 = serial)
}

func (o Options) maxIters() int {
	if o.MaxIters > 0 {
		return o.MaxIters
	}
	return 6
}

func (o Options) system() string {
	if o.System != "" {
		return o.System
	}
	return DefaultSystem
}

func (o Options) concurrency() int {
	if o.Concurrency > 1 {
		return o.Concurrency
	}
	return 1
}

// ArmResult is one arm's answer plus its graded verdict and real cost. Err is set
// (and Verdict is Failed) when the arm errored or timed out before answering.
// Scored is false when the arm was out of remit for the question — its data view
// cannot answer it (the raw arm on an hdf-native question) — so it is excluded
// from accuracy and its answer is reported only as hallucinate-vs-abstain.
type ArmResult struct {
	Arm     Arm
	Answer  string
	Verdict Verdict
	Scored  bool
	Err     string
	Cost    agent.Result
}

// QuestionResult is one graded question across the arms.
type QuestionResult struct {
	ID       string
	Ask      string
	Class    truth.Class
	RawView  truth.Answer
	HDFView  truth.Answer
	Raw      ArmResult
	HDF      ArmResult  // pipeline (amortized) view
	HDFAdHoc *ArmResult // conversion-included view, if Options.AdHoc
}

// adHocTools is the HDF arm's ad-hoc surface: the read tools plus hdf_convert, so
// the agent can normalize on demand (the conversion-included workflow).
func adHocTools() map[string]bool {
	m := map[string]bool{"hdf_convert": true}
	for k, v := range agent.ReadTools {
		m[k] = v
	}
	return m
}

// Run executes the study for every question in bank against one model. sess must
// be connected with writes enabled (HDF_MCP_ENABLE_WRITES=1) and HDF_MCP_ROOT set
// to root; fixturesDir holds the raw fixtures. Questions are graded concurrently
// (bounded by Options.Concurrency); a per-question/arm failure is recorded, never
// fatal. Only setup errors (staging/conversion) abort.
func Run(ctx context.Context, inst instrument.Instrument, sess *mcpclient.Session, fixturesDir, root string, bank []Question, opts Options) ([]QuestionResult, error) {
	rawBytes, err := stageAndConvert(ctx, sess, fixturesDir, root, bank)
	if err != nil {
		return nil, err
	}

	rawTB := agent.NewRawFileToolBox(root)
	mcpTB, err := agent.NewMCPToolBox(ctx, sess, agent.ReadTools)
	if err != nil {
		return nil, fmt.Errorf("build hdf toolbox: %w", err)
	}
	var adHocTB *agent.MCPToolBox
	if opts.AdHoc {
		if adHocTB, err = agent.NewMCPToolBox(ctx, sess, adHocTools()); err != nil {
			return nil, fmt.Errorf("build ad-hoc toolbox: %w", err)
		}
	}

	results := make([]QuestionResult, len(bank))
	errs := make([]error, len(bank))
	sem := make(chan struct{}, opts.concurrency())
	var wg sync.WaitGroup
	for i, q := range bank {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, q Question) {
			defer wg.Done()
			defer func() { <-sem }()
			qr, qerr := runQuestion(ctx, inst, rawTB, mcpTB, adHocTB, root, rawBytes[q.Fixture], q, opts)
			results[i], errs[i] = qr, qerr
		}(i, q)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			return nil, fmt.Errorf("[%s] %w", bank[i].ID, e)
		}
	}
	return results, nil
}

// runQuestion classifies one question and runs its arms. Arm failures become
// recorded outcomes; only a setup error (reading the converted doc, classifying)
// is returned as fatal.
func runQuestion(ctx context.Context, inst instrument.Instrument, rawTB, mcpTB agent.ToolBox, adHocTB *agent.MCPToolBox, root string, raw []byte, q Question, opts Options) (QuestionResult, error) {
	hdfDoc, err := os.ReadFile(filepath.Join(root, q.HDFName))
	if err != nil {
		return QuestionResult{}, fmt.Errorf("read converted %s: %w", q.HDFName, err)
	}
	cls, err := truth.Classify(q.Truth, raw, hdfDoc)
	if err != nil {
		return QuestionResult{}, err
	}

	qr := QuestionResult{ID: q.ID, Ask: q.Ask, Class: cls.Class, RawView: cls.RawAnswer, HDFView: cls.HDFAnswer}
	qr.Raw = runArm(ctx, inst, rawTB, ArmRaw, opts,
		q.Ask+" The scan file is named "+q.Fixture+".",
		q.key(cls.Class, cls.RawAnswer, cls.HDFAnswer, ArmRaw), q.Kind)
	hdfKey := q.key(cls.Class, cls.RawAnswer, cls.HDFAnswer, ArmHDF)
	qr.HDF = runArm(ctx, inst, mcpTB, ArmHDF, opts,
		q.Ask+" The HDF document is named "+q.HDFName+".", hdfKey, q.Kind)

	if adHocTB != nil {
		// A per-question output name keeps concurrent ad-hoc conversions from
		// colliding in the shared root. The raw file is already staged.
		out := q.ID + ".adhoc.hdf.json"
		ah := runArm(ctx, inst, adHocTB, ArmHDF, opts,
			fmt.Sprintf("%s The raw scan file is named %s. First convert it to HDF using hdf_convert with from=%s and output=%s, then analyze that HDF document.",
				q.Ask, q.Fixture, q.From, out), hdfKey, q.Kind)
		qr.HDFAdHoc = &ah
	}
	return qr, nil
}

// runArm runs one agent arm and grades its answer. An error (including a timeout)
// is captured as a Failed outcome with whatever tokens were spent — never
// propagated, so one over-context arm can't sink the whole run.
func runArm(ctx context.Context, inst instrument.Instrument, tb agent.ToolBox, arm Arm, opts Options, prompt string, key truth.Answer, kind Kind) ArmResult {
	res, err := agent.Run(ctx, inst, tb, opts.system(), prompt, opts.maxIters())
	if err != nil {
		return ArmResult{Arm: arm, Answer: res.Answer, Verdict: Failed, Scored: key.Answerable, Err: err.Error(), Cost: res}
	}
	return ArmResult{Arm: arm, Answer: res.Answer, Verdict: Grade(res.Answer, key, kind), Scored: key.Answerable, Cost: res}
}

// stageAndConvert copies each referenced raw fixture under root and normalizes it
// to HDF via the MCP hdf_convert tool. It returns the raw bytes by filename. Runs
// once, serially, before the concurrent phase.
func stageAndConvert(ctx context.Context, sess *mcpclient.Session, fixturesDir, root string, bank []Question) (map[string][]byte, error) {
	raw := map[string][]byte{}
	for _, q := range bank {
		if _, done := raw[q.Fixture]; done {
			continue
		}
		b, err := os.ReadFile(filepath.Join(fixturesDir, q.Fixture))
		if err != nil {
			return nil, fmt.Errorf("read fixture %s: %w", q.Fixture, err)
		}
		if err := os.WriteFile(filepath.Join(root, q.Fixture), b, 0o600); err != nil {
			return nil, fmt.Errorf("stage fixture %s: %w", q.Fixture, err)
		}
		raw[q.Fixture] = b
		if _, err := sess.Call(ctx, "hdf_convert", map[string]any{
			"source": map[string]any{"path": q.Fixture}, "from": q.From, "output": q.HDFName,
		}); err != nil {
			return nil, fmt.Errorf("normalize %s: %w", q.Fixture, err)
		}
	}
	return raw, nil
}

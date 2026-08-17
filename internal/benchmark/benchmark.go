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
// abstention over false credit, and Class B is scored bidirectionally.
package benchmark

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	MaxIters int    // tool round-trip cap per arm (default 6)
	System   string // system prompt (default DefaultSystem)
	AdHoc    bool   // also measure the conversion-included HDF cost view
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

// ArmResult is one arm's answer plus its graded verdict and real cost.
type ArmResult struct {
	Arm     Arm
	Answer  string
	Verdict Verdict
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
// to root; fixturesDir holds the raw fixtures. The pipeline HDF arm reads
// documents this function pre-converts; the ad-hoc arm converts them itself in a
// separate root so its cost includes the conversion round-trip.
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
	var adHocRoot string
	if opts.AdHoc {
		adHocTB, err = agent.NewMCPToolBox(ctx, sess, adHocTools())
		if err != nil {
			return nil, fmt.Errorf("build ad-hoc toolbox: %w", err)
		}
	}

	var out []QuestionResult
	for _, q := range bank {
		hdfDoc, err := os.ReadFile(filepath.Join(root, q.HDFName))
		if err != nil {
			return nil, fmt.Errorf("read converted %s: %w", q.HDFName, err)
		}
		cls, err := truth.Classify(q.Truth, rawBytes[q.Fixture], hdfDoc)
		if err != nil {
			return nil, err
		}

		qr := QuestionResult{ID: q.ID, Ask: q.Ask, Class: cls.Class, RawView: cls.RawAnswer, HDFView: cls.HDFAnswer}

		rawKey := q.key(cls.Class, cls.RawAnswer, cls.HDFAnswer, ArmRaw)
		qr.Raw, err = runArm(ctx, inst, rawTB, ArmRaw, opts,
			q.Ask+" The scan file is named "+q.Fixture+".", rawKey, q.Kind)
		if err != nil {
			return nil, fmt.Errorf("[%s] raw arm: %w", q.ID, err)
		}

		hdfKey := q.key(cls.Class, cls.RawAnswer, cls.HDFAnswer, ArmHDF)
		qr.HDF, err = runArm(ctx, inst, mcpTB, ArmHDF, opts,
			q.Ask+" The HDF document is named "+q.HDFName+".", hdfKey, q.Kind)
		if err != nil {
			return nil, fmt.Errorf("[%s] hdf arm: %w", q.ID, err)
		}

		if opts.AdHoc {
			if adHocRoot == "" {
				if adHocRoot, err = os.MkdirTemp("", "hdf-mcp-demo-adhoc-"); err != nil {
					return nil, err
				}
				defer os.RemoveAll(adHocRoot)
			}
			// Stage only the raw file; the agent must convert it itself.
			if err := os.WriteFile(filepath.Join(root, q.Fixture), rawBytes[q.Fixture], 0o600); err != nil {
				return nil, err
			}
			ah, err := runArm(ctx, inst, adHocTB, ArmHDF, opts,
				fmt.Sprintf("%s The raw scan file is named %s. First convert it to HDF using hdf_convert with from=%s and output=%s, then analyze that HDF document.",
					q.Ask, q.Fixture, q.From, q.HDFName), hdfKey, q.Kind)
			if err != nil {
				return nil, fmt.Errorf("[%s] ad-hoc arm: %w", q.ID, err)
			}
			qr.HDFAdHoc = &ah
		}

		out = append(out, qr)
	}
	return out, nil
}

// runArm runs one agent arm and grades its answer.
func runArm(ctx context.Context, inst instrument.Instrument, tb agent.ToolBox, arm Arm, opts Options, prompt string, key truth.Answer, kind Kind) (ArmResult, error) {
	res, err := agent.Run(ctx, inst, tb, opts.system(), prompt, opts.maxIters())
	if err != nil {
		return ArmResult{}, err
	}
	return ArmResult{Arm: arm, Answer: res.Answer, Verdict: Grade(res.Answer, key, kind), Cost: res}, nil
}

// stageAndConvert copies each referenced raw fixture under root and normalizes it
// to HDF via the MCP hdf_convert tool. It returns the raw bytes by filename.
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

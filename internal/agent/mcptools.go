package agent

import (
	"context"
	"encoding/json"

	"github.com/mitre/hdf-mcp-demo/internal/instrument"
	"github.com/mitre/hdf-mcp-demo/internal/mcpclient"
)

// ReadTools is the realistic read/analysis surface a querying agent needs. The
// write tools (hdf_author, hdf_apply_amendment) and hdf_convert are excluded: the
// HDF arm operates on already-normalized documents (the amortized pipeline case).
var ReadTools = map[string]bool{
	"hdf_open":       true,
	"hdf_inspect":    true,
	"hdf_query":      true,
	"hdf_compliance": true,
	"hdf_diff":       true,
	"hdf_validate":   true,
}

// MCPToolBox is the HDF arm's tool surface: the real HDF MCP tools, taken live
// from the server's tools/list and executed against the shared session. The
// model therefore carries the actual HDF tool schemas in its context — a real
// cost the study counts honestly.
type MCPToolBox struct {
	sess *mcpclient.Session
	defs []instrument.Tool
}

// NewMCPToolBox builds the toolbox from the session's tools/list. If allow is
// non-nil, only tools whose names are in it are exposed (e.g. ReadTools).
func NewMCPToolBox(ctx context.Context, sess *mcpclient.Session, allow map[string]bool) (*MCPToolBox, error) {
	tds, err := sess.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	var defs []instrument.Tool
	for _, td := range tds {
		if allow != nil && !allow[td.Name] {
			continue
		}
		defs = append(defs, instrument.Tool{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  td.InputSchema,
		})
	}
	return &MCPToolBox{sess: sess, defs: defs}, nil
}

// Definitions returns the exposed HDF tools.
func (b *MCPToolBox) Definitions() []instrument.Tool { return b.defs }

// Execute routes a tool call to the MCP server and returns its structured
// response as JSON (what the model sees).
func (b *MCPToolBox) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	var m map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &m); err != nil {
			return "", err
		}
	}
	return b.sess.Call(ctx, name, m)
}

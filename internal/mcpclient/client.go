// Package mcpclient is a thin external MCP client over the shipped `hdf mcp`
// binary. It spawns the binary and speaks JSON-RPC over stdio via the public MCP
// SDK — the same path any real consumer uses. The demo depends on nothing
// private to hdf-libs; that constraint (a separate module cannot import
// hdf-cli/internal/*) and the honest "this is how you consume it" demonstration
// are the same thing.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stderrTail is how many bytes of the server's stderr to retain. The server logs
// to stderr by contract (stdout carries JSON-RPC), and without capturing it a
// crashed server is indistinguishable from any other transport EOF — the client
// just reports "connection closed" and the actual cause is lost.
const stderrTail = 8 << 10

// ringWriter keeps only the last n bytes written to it, so a chatty server
// cannot grow the buffer without bound over a long run.
type ringWriter struct {
	mu  sync.Mutex
	buf []byte
	n   int
}

func (w *ringWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.n {
		w.buf = w.buf[len(w.buf)-w.n:]
	}
	return len(p), nil
}

func (w *ringWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

// Session is a connected MCP client over a spawned `hdf mcp` process. Call is
// mutex-guarded so concurrent arms (the parallel benchmark) can share one session
// safely; tool calls are fast local library work, so serializing them is cheap
// while the slow model calls that bracket them still run concurrently.
type Session struct {
	cs     *sdkmcp.ClientSession
	mu     sync.Mutex
	errLog *ringWriter
}

// ServerLog returns the tail of the server's stderr. It is the first thing to
// look at when a call fails with a transport error: an EOF means the process is
// gone, and its dying words are here.
func (s *Session) ServerLog() string {
	if s.errLog == nil {
		return ""
	}
	return s.errLog.String()
}

// Connect spawns `binary mcp` and connects an MCP client over its stdio. env
// entries are "KEY=VALUE" (e.g. "HDF_MCP_ROOT=/path"); pass nil to inherit the
// parent environment. The caller must Close the returned session.
func Connect(ctx context.Context, binary string, env []string) (*Session, error) {
	cmd := exec.Command(binary, "mcp")
	if env != nil {
		cmd.Env = env
	}
	errLog := &ringWriter{n: stderrTail}
	cmd.Stderr = errLog
	c := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "hdf-mcp-demo", Version: "0.1.0"}, nil)
	cs, err := c.Connect(ctx, &sdkmcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %q mcp over stdio: %w", binary, err)
	}
	return &Session{cs: cs, errLog: errLog}, nil
}

// Call invokes a tool and returns its structured content as compact JSON — the
// bytes an agent's context actually receives, and what the demo tokenizes. It
// returns an error on an isError tool result.
func (s *Session) Call(ctx context.Context, name string, args map[string]any) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.cs.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	if res.IsError {
		return "", fmt.Errorf("tool %q returned an error: %s", name, firstText(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return "", fmt.Errorf("marshal %q structured content: %w", name, err)
	}
	return string(b), nil
}

// ToolDef is a tool the server advertises: name, description, and its input
// JSON-Schema (as a generic map, ready to hand to a model as a function schema).
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// ListTools returns the server's advertised tools (tools/list) — used to hand a
// model the real HDF tool schemas for the HDF arm of the study.
func (s *Session) ListTools(ctx context.Context) ([]ToolDef, error) {
	res, err := s.cs.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}
	defs := make([]ToolDef, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := map[string]any{}
		if t.InputSchema != nil {
			b, mErr := json.Marshal(t.InputSchema)
			if mErr == nil {
				_ = json.Unmarshal(b, &schema)
			}
		}
		defs = append(defs, ToolDef{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return defs, nil
}

// Close shuts down the session and terminates the spawned process.
func (s *Session) Close() error { return s.cs.Close() }

func firstText(res *sdkmcp.CallToolResult) string {
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*sdkmcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

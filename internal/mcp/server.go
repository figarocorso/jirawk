// Package mcp implements a small stdio JSON-RPC 2.0 server that speaks the
// Model Context Protocol. Only the subset jirawk needs is implemented:
// initialize, tools/list, tools/call (plus the initialized notification).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/figarocorso/jirawk/internal/config"
	"github.com/figarocorso/jirawk/internal/jira"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "jirawk"
)

// Server holds the wiring for a single MCP session.
type Server struct {
	cfg           *config.Config
	client        jira.Client
	in            io.Reader
	out           io.Writer
	serverVersion string
}

// Options is the dependency bag for NewServer.
type Options struct {
	Config  *config.Config
	Client  jira.Client
	In      io.Reader
	Out     io.Writer
	Version string
}

// NewServer constructs a Server from Options, defaulting In/Out to stdio.
func NewServer(opts Options) *Server {
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	return &Server{
		cfg:           opts.Config,
		client:        opts.Client,
		in:            opts.In,
		out:           opts.Out,
		serverVersion: opts.Version,
	}
}

// Run reads JSON-RPC messages line-by-line from In and writes responses to Out.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if err := s.handleLine(ctx, line); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (s *Server) handleLine(ctx context.Context, line []byte) error {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return s.writeErr(nil, -32700, "parse error", err.Error())
	}
	isNotification := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "initialize":
		return s.writeResult(req.ID, s.handleInitialize())
	case "initialized", "notifications/initialized":
		return nil
	case "tools/list":
		return s.writeResult(req.ID, map[string]any{"tools": s.toolDefinitions()})
	case "tools/call":
		result, err := s.handleToolCall(ctx, req.Params)
		if err != nil {
			return s.writeErr(req.ID, -32603, "tool error", err.Error())
		}
		return s.writeResult(req.ID, result)
	default:
		if isNotification {
			return nil
		}
		return s.writeErr(req.ID, -32601, "method not found", req.Method)
	}
}

func (s *Server) handleInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": serverName, "version": s.serverVersion},
	}
}

func (s *Server) writeResult(id json.RawMessage, result any) error {
	return s.encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeErr(id json.RawMessage, code int, msg string, data any) error {
	return s.encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}})
}

func (s *Server) encode(resp rpcResponse) error {
	if resp.ID == nil {
		resp.ID = json.RawMessage("null")
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := s.out.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

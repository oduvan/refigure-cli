// Package mcp serves the Model Context Protocol over a byte stream.
//
// It is here so `refigure mcp` can offer an agent the same jobs the command
// line offers a build script. There is no MCP library behind it for the same
// reason the whole repository has five dependencies: on this transport the
// protocol is newline-delimited JSON-RPC, and encoding/json already does the
// part that is worth importing something for.
//
// Nothing in this package prints. Serve reads and writes the two streams it is
// handed, which keeps `main` the only package that touches os.Stdout — and lets
// the whole protocol be tested through a pair of buffers rather than a
// subprocess.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// The protocol split into two eras with revision 2026-07-28. A "modern" client
// states its version in every request's `_meta` and asks `server/discover` what
// the server can do; a "legacy" one opens with an `initialize` handshake and
// asks nothing again. This server answers both, because which one it will meet
// depends entirely on how old the client is, and it costs a handful of lines to
// stop caring.
const (
	latestVersion = "2026-07-28"
	newestLegacy  = "2025-11-25"
)

// supportedVersions is what `server/discover` advertises and what an
// unsupported-version error lists, newest first.
var supportedVersions = []string{latestVersion, newestLegacy, "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC codes, plus the one the protocol adds for a version it will not
// speak.
const (
	codeParse             = -32700
	codeInvalidRequest    = -32600
	codeMethodNotFound    = -32601
	codeInvalidParams     = -32602
	codeUnsupportedVerson = -32022
)

// metaVersion is where a modern request states the revision it is speaking.
const metaVersion = "io.modelcontextprotocol/protocolVersion"

// Content is one block of a tool's answer: text, or an image the client shows
// to the model.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// Text is a block of prose for the model to read.
func Text(message string, args ...any) Content {
	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}
	return Content{Type: "text", Text: message}
}

// Image is a picture for the model to look at. The bytes are base64-encoded on
// the way out, which is what makes an image worth roughly a third more than it
// weighs — callers should send something small.
func Image(data []byte, mimeType string) Content {
	return Content{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType}
}

// Result is what a tool answers with.
type Result struct {
	Content []Content `json:"content"`
	// Structured is the same answer as data, for a caller that would otherwise
	// parse the text. It is omitted when a tool has nothing machine-readable
	// to say.
	Structured any `json:"structuredContent,omitempty"`
	// IsError marks a failure the model can act on — a project that does not
	// validate, a cut that is not there. A tool returning an error gets this
	// set for it. Protocol-level failures are JSON-RPC errors instead, because
	// no rewording of the arguments will fix them.
	IsError bool `json:"isError,omitempty"`
}

// Property is one argument a tool takes. Type is a JSON Schema type, with
// "string[]" as shorthand for an array of strings.
type Property struct {
	Name        string
	Type        string
	Description string
	Enum        []string
	Required    bool
}

// Annotations are hints a client may show a person before it lets a model run
// the tool. All four are stated even where the value is the protocol's own
// default: a client reading them is deciding whether to ask permission, and
// "unstated" and "false" mean different things to the person being asked.
type Annotations struct {
	ReadOnly    bool
	Destructive bool
	Idempotent  bool
	OpenWorld   bool
}

// Tool is one job this server offers.
type Tool struct {
	Name        string
	Title       string
	Description string
	Arguments   []Property
	Annotations Annotations
	// Call runs the job. A returned error becomes an IsError result, so the
	// model reads the message and can try again with better arguments.
	Call func(arguments json.RawMessage) (Result, error)
}

// Server offers a fixed set of tools over one stream.
type Server struct {
	Name    string
	Version string
	// Instructions is what the client may put in front of the model before it
	// sees the tools: what this server is for, and the rules it cannot guess
	// from the tool names.
	Instructions string
	Tools        []Tool
}

// Serve answers messages until in reaches end of file.
//
// Requests are handled one at a time. Exporting is the slowest thing here and
// it is CPU-bound, so answering two at once would not make either finish
// sooner, and a single-threaded server cannot interleave two writes into one
// stream by accident.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	encoder := json.NewEncoder(out)
	// Without this, every `<` in a message comes out as < — valid, and
	// unreadable in a log.
	encoder.SetEscapeHTML(false)

	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if answer := s.handle(line); answer != nil {
				// Encode writes compact JSON and one newline, which is exactly
				// the framing this transport asks for: a message per line, and
				// no newline inside one.
				if err := encoder.Encode(answer); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
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

// handle answers one message, or returns nil for one that must not be answered.
func (s *Server) handle(line []byte) *response {
	var message request
	if err := json.Unmarshal(line, &message); err != nil {
		return failure(json.RawMessage("null"), codeParse, "the message is not valid JSON", nil)
	}
	// A notification carries no id and must never be answered — including the
	// `notifications/initialized` a legacy client sends once the handshake is
	// done, and the `notifications/cancelled` it sends to abandon a request.
	if isNotification(message.ID) {
		return nil
	}
	if message.Method == "" {
		return failure(message.ID, codeInvalidRequest, "the message names no method", nil)
	}

	version := statedVersion(message.Params)
	if version != "" && !isSupported(version) {
		return failure(message.ID, codeUnsupportedVerson, "unsupported protocol version", map[string]any{
			"supported": supportedVersions,
			"requested": version,
		})
	}
	// A modern result says so; a legacy client would only see a field it has
	// no schema for.
	modern := version == latestVersion

	switch message.Method {
	case "initialize":
		// The legacy handshake. Answering it is what makes the version in
		// `serverInfo` and the instructions reach an older client at all.
		return success(message.ID, map[string]any{
			"protocolVersion": negotiate(message.Params),
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
			"instructions":    s.Instructions,
		})

	case "server/discover":
		// Modern clients also use this as the probe that tells them which era
		// they are talking to, so it always answers in modern shape.
		return success(message.ID, complete(map[string]any{
			"supportedVersions": supportedVersions,
			"capabilities":      map[string]any{"tools": map[string]any{}},
			"instructions":      s.Instructions,
			"_meta": map[string]any{
				"io.modelcontextprotocol/serverInfo": map[string]any{
					"name":    s.Name,
					"version": s.Version,
				},
			},
		}, true))

	case "tools/list":
		return success(message.ID, complete(map[string]any{"tools": s.describe()}, modern))

	case "tools/call":
		return s.call(message, modern)

	case "ping":
		return success(message.ID, complete(map[string]any{}, modern))

	default:
		return failure(message.ID, codeMethodNotFound, "this server has no method "+message.Method, nil)
	}
}

func (s *Server) call(message request, modern bool) *response {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(message.Params) > 0 {
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return failure(message.ID, codeInvalidParams, "the call parameters are not an object", nil)
		}
	}

	for i := range s.Tools {
		tool := &s.Tools[i]
		if tool.Name != params.Name {
			continue
		}
		result := run(tool, params.Arguments)
		if modern {
			return success(message.ID, toolResponse{Result: result, ResultType: "complete"})
		}
		return success(message.ID, result)
	}
	return failure(message.ID, codeInvalidParams, "this server has no tool named "+params.Name, nil)
}

// run calls one tool and turns whatever comes back — including a panic — into a
// result. A server that dies on one bad project takes every other tool with it
// and leaves the client to notice and restart; an error the model can read
// costs nothing and is recoverable.
func run(tool *Tool, arguments json.RawMessage) (result Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Result{
				Content: []Content{Text("%s failed: %v", tool.Name, recovered)},
				IsError: true,
			}
		}
	}()

	result, err := tool.Call(arguments)
	if err != nil {
		return Result{Content: []Content{Text("%s", err)}, IsError: true}
	}
	return result
}

// describe is the tool list, in the order they were declared — the protocol
// asks for a stable order so a client can cache it.
func (s *Server) describe() []map[string]any {
	described := make([]map[string]any, 0, len(s.Tools))
	for _, tool := range s.Tools {
		entry := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schemaFor(tool.Arguments),
			"annotations": map[string]any{
				"readOnlyHint":    tool.Annotations.ReadOnly,
				"destructiveHint": tool.Annotations.Destructive,
				"idempotentHint":  tool.Annotations.Idempotent,
				"openWorldHint":   tool.Annotations.OpenWorld,
			},
		}
		if tool.Title != "" {
			// Both places: the field moved to the top of a tool in a later
			// revision, and an older client only looks in the annotations.
			entry["title"] = tool.Title
			entry["annotations"].(map[string]any)["title"] = tool.Title
		}
		described = append(described, entry)
	}
	return described
}

func schemaFor(arguments []Property) map[string]any {
	properties := map[string]any{}
	var required []string

	for _, argument := range arguments {
		entry := map[string]any{"description": argument.Description}
		if item, isList := strings.CutSuffix(argument.Type, "[]"); isList {
			items := map[string]any{"type": item}
			if len(argument.Enum) > 0 {
				items["enum"] = argument.Enum
			}
			entry["type"] = "array"
			entry["items"] = items
		} else {
			entry["type"] = argument.Type
			if len(argument.Enum) > 0 {
				entry["enum"] = argument.Enum
			}
		}
		properties[argument.Name] = entry
		if argument.Required {
			required = append(required, argument.Name)
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		// Saying so turns a misspelled argument into a client-side complaint
		// the model can read, rather than a silent default.
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// negotiate picks the revision to answer a legacy handshake in: the one asked
// for when this server speaks it, and otherwise the newest it has. A client
// that cannot speak the answer will say so; there is nothing else to offer it.
func negotiate(params json.RawMessage) string {
	var opening struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		_ = json.Unmarshal(params, &opening)
	}
	for _, version := range supportedVersions {
		// The newest revision is not a handshake revision, so a client asking
		// for it here gets the newest one that is.
		if version != latestVersion && version == opening.ProtocolVersion {
			return version
		}
	}
	return newestLegacy
}

// statedVersion is the revision a modern request declares. Empty means the
// request did not say, which is how every legacy request looks.
func statedVersion(params json.RawMessage) string {
	if len(params) == 0 {
		return ""
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return ""
	}
	raw, ok := envelope.Meta[metaVersion]
	if !ok {
		return ""
	}
	var version string
	if err := json.Unmarshal(raw, &version); err != nil {
		return ""
	}
	return version
}

func isSupported(version string) bool {
	for _, supported := range supportedVersions {
		if supported == version {
			return true
		}
	}
	return false
}

// isNotification reports whether a message must be left unanswered. JSON-RPC
// says a notification omits the id; an explicit null is not one, but no client
// sends it meaning anything else, and a reply nobody can correlate is worse
// than silence.
func isNotification(id json.RawMessage) bool {
	trimmed := bytes.TrimSpace(id)
	return len(trimmed) == 0 || string(trimmed) == "null"
}

func success(id json.RawMessage, result any) *response {
	return &response{JSONRPC: "2.0", ID: id, Result: result}
}

// complete stamps a result as final. The newest revision requires it and an
// older client has no schema for it, so it is added only for a request that
// arrived speaking the newest revision.
func complete(result map[string]any, modern bool) map[string]any {
	if modern {
		result["resultType"] = "complete"
	}
	return result
}

// toolResponse is a tool's own result with that same stamp on it. Embedding
// keeps the stamp out of the Result a tool writes, and out of every tool that
// has no business knowing which revision asked.
type toolResponse struct {
	Result
	ResultType string `json:"resultType,omitempty"`
}

func failure(id json.RawMessage, code int, message string, data any) *response {
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message, Data: data}}
}

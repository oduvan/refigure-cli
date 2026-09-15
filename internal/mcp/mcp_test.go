// The protocol, driven through a pair of buffers.
//
// What is worth testing here is the part a client depends on and a person
// cannot see: that a notification is never answered, that a tool's own failure
// and a broken request come back as different things, and that every message is
// one line. Getting any of those wrong hangs or confuses a client with no error
// anywhere to read.
package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testServer(tools ...Tool) *Server {
	return &Server{
		Name:         "refigure",
		Version:      "v9.9.9-test",
		Instructions: "what this server is for",
		Tools:        tools,
	}
}

// serve runs one session and returns the raw lines that came back.
func serve(t *testing.T, server *Server, messages ...string) []string {
	t.Helper()
	var out strings.Builder
	if err := server.Serve(strings.NewReader(strings.Join(messages, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serving failed: %v", err)
	}
	written := out.String()
	if written == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(written, "\n"), "\n")
}

func decode(t *testing.T, line string) map[string]any {
	t.Helper()
	var message map[string]any
	if err := json.Unmarshal([]byte(line), &message); err != nil {
		t.Fatalf("the server wrote something that is not JSON: %q", line)
	}
	return message
}

func result(t *testing.T, line string) map[string]any {
	t.Helper()
	message := decode(t, line)
	if failure, ok := message["error"]; ok {
		t.Fatalf("expected a result, got error %v", failure)
	}
	value, ok := message["result"].(map[string]any)
	if !ok {
		t.Fatalf("the reply carries no result object: %q", line)
	}
	return value
}

func rpcFailure(t *testing.T, line string) map[string]any {
	t.Helper()
	message := decode(t, line)
	value, ok := message["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error, got %q", line)
	}
	return value
}

// A tool that answers with whatever it was handed, so a test can see that the
// arguments arrived and how many times it ran.
func echoTool(calls *int) Tool {
	return Tool{
		Name:        "echo",
		Title:       "Echo",
		Description: "says what it was told",
		Arguments: []Property{
			{Name: "say", Type: "string", Description: "the words", Required: true},
			{Name: "times", Type: "integer", Description: "how often"},
			{Name: "tags", Type: "string[]", Description: "labels", Enum: []string{"a", "b"}},
		},
		Annotations: Annotations{ReadOnly: true, Idempotent: true},
		Call: func(arguments json.RawMessage) (Result, error) {
			if calls != nil {
				*calls++
			}
			var args struct {
				Say string `json:"say"`
			}
			_ = json.Unmarshal(arguments, &args)
			return Result{
				Content:    []Content{Text("%s", args.Say)},
				Structured: map[string]any{"said": args.Say},
			}, nil
		},
	}
}

// The one that hangs a client when it is wrong: a client that sent a
// notification is not waiting for anything, and a reply to it arrives as the
// answer to whatever it asks next.
func TestANotificationIsNeverAnswered(t *testing.T) {
	lines := serve(t, testServer(echoTool(nil)),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(lines) != 1 {
		t.Fatalf("answered %d messages, want only the one carrying an id: %v", len(lines), lines)
	}
	if id := decode(t, lines[0])["id"]; id != float64(7) {
		t.Errorf("answered id %v, want 7", id)
	}
}

// The transport frames messages by newline, so one inside a message would split
// it in two and leave the client with half a document.
func TestEveryMessageIsOneLine(t *testing.T) {
	server := testServer(Tool{
		Name:        "multiline",
		Description: "answers with several lines",
		Call: func(json.RawMessage) (Result, error) {
			return Result{Content: []Content{Text("first\nsecond\nthird")}}, nil
		},
	})
	lines := serve(t, server, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"multiline"}}`)
	if len(lines) != 1 {
		t.Fatalf("one answer came back as %d lines: %v", len(lines), lines)
	}
	content := result(t, lines[0])["content"].([]any)[0].(map[string]any)
	if content["text"] != "first\nsecond\nthird" {
		t.Errorf("the newlines did not survive the trip: %q", content["text"])
	}
}

// A client that opens with the handshake is told a revision this server speaks.
func TestTheHandshakeAnswersWithARevisionItSpeaks(t *testing.T) {
	for _, testCase := range []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2024-11-05", "2024-11-05"},
		// Not a handshake revision, so the newest one that is.
		{"2026-07-28", newestLegacy},
		{"1900-01-01", newestLegacy},
		{"", newestLegacy},
	} {
		lines := serve(t, testServer(),
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+testCase.asked+`"}}`)
		got := result(t, lines[0])
		if got["protocolVersion"] != testCase.want {
			t.Errorf("asked for %q, was answered %v, want %q", testCase.asked, got["protocolVersion"], testCase.want)
		}
		if got["instructions"] != "what this server is for" {
			t.Error("the handshake dropped the instructions")
		}
		capabilities := got["capabilities"].(map[string]any)
		if _, ok := capabilities["tools"]; !ok {
			t.Error("the handshake does not declare the tools capability")
		}
	}
}

// The newest revision has no handshake at all: a client asks what the server
// can do, and states its revision on every request afterwards.
func TestDiscoveryNamesEveryRevisionThisServerSpeaks(t *testing.T) {
	lines := serve(t, testServer(), `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"`+latestVersion+`"}}}`)
	got := result(t, lines[0])

	versions := got["supportedVersions"].([]any)
	if len(versions) != len(supportedVersions) || versions[0] != latestVersion {
		t.Fatalf("discovery advertised %v", versions)
	}
	// Discovery is a request only the newest revision makes, so it always
	// answers in that revision's shape.
	if got["resultType"] != "complete" {
		t.Error("a discovery result is not marked complete")
	}
	info := got["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if info["name"] != "refigure" || info["version"] != "v9.9.9-test" {
		t.Errorf("discovery reported %v", info)
	}
}

// Results are stamped for the revision that asked. An older client has no
// schema for the stamp, and the newest one requires it.
func TestOnlyTheNewestRevisionIsAnsweredWithAStamp(t *testing.T) {
	modern := serve(t, testServer(echoTool(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"`+latestVersion+`"},"name":"echo","arguments":{"say":"hi"}}}`)
	if result(t, modern[0])["resultType"] != "complete" {
		t.Error("a result for the newest revision is not marked complete")
	}

	legacy := serve(t, testServer(echoTool(nil)),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"say":"hi"}}}`)
	if _, stamped := result(t, legacy[0])["resultType"]; stamped {
		t.Error("a result for a client that never asked for the newest revision carries its stamp")
	}
}

// A revision this server does not speak is refused with the list to choose
// from — and refused before the tool runs, so nothing happens on the way to
// finding out.
func TestAnUnknownRevisionIsRefusedBeforeTheToolRuns(t *testing.T) {
	calls := 0
	lines := serve(t, testServer(echoTool(&calls)),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1900-01-01"},"name":"echo","arguments":{"say":"hi"}}}`)

	failure := rpcFailure(t, lines[0])
	if failure["code"] != float64(codeUnsupportedVerson) {
		t.Errorf("refused with code %v, want %d", failure["code"], codeUnsupportedVerson)
	}
	data := failure["data"].(map[string]any)
	if data["requested"] != "1900-01-01" || len(data["supported"].([]any)) != len(supportedVersions) {
		t.Errorf("the refusal does not say what to use instead: %v", data)
	}
	if calls != 0 {
		t.Errorf("the tool ran %d times for a request that was refused", calls)
	}
}

// The two kinds of failure are not the same kind, and a client treats them
// differently: one is fed back to the model to fix, the other is not.
func TestAToolsOwnFailureIsAResultAndABadRequestIsAnError(t *testing.T) {
	server := testServer(Tool{
		Name:        "grumpy",
		Description: "always refuses",
		Call: func(json.RawMessage) (Result, error) {
			return Result{}, errors.New("no cut is named \"login\"")
		},
	})

	lines := serve(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"grumpy"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"absent"}}`,
		`{"jsonrpc":"2.0","id":3,"method":"nonsense/method"}`,
	)

	failed := result(t, lines[0])
	if failed["isError"] != true {
		t.Error("a tool that failed did not come back as an error result")
	}
	text := failed["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "login") {
		t.Errorf("the model is not told why: %q", text)
	}

	if code := rpcFailure(t, lines[1])["code"]; code != float64(codeInvalidParams) {
		t.Errorf("an unknown tool answered %v, want %d", code, codeInvalidParams)
	}
	if code := rpcFailure(t, lines[2])["code"]; code != float64(codeMethodNotFound) {
		t.Errorf("an unknown method answered %v, want %d", code, codeMethodNotFound)
	}
}

// One bad project must not take the server down with it: the client would have
// to notice the process had gone and start another, having lost the answer to
// whatever else was in flight.
func TestAToolThatPanicsIsAnErrorAndTheServerKeepsGoing(t *testing.T) {
	server := testServer(
		Tool{
			Name:        "boom",
			Description: "explodes",
			Call:        func(json.RawMessage) (Result, error) { panic("off the end of a slice") },
		},
		echoTool(nil),
	)

	lines := serve(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"say":"still here"}}}`,
	)
	if len(lines) != 2 {
		t.Fatalf("got %d answers, want 2: %v", len(lines), lines)
	}
	if result(t, lines[0])["isError"] != true {
		t.Error("a panic did not come back as an error result")
	}
	text := result(t, lines[1])["content"].([]any)[0].(map[string]any)["text"]
	if text != "still here" {
		t.Errorf("the server did not answer the next request: %v", text)
	}
}

// The schema is what a client validates a model's arguments against, so a
// mistyped argument is caught before it reaches a tool that ignores it.
func TestToolsDescribeTheArgumentsTheyTake(t *testing.T) {
	lines := serve(t, testServer(echoTool(nil)), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := result(t, lines[0])["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("listed %d tools, want 1", len(tools))
	}
	tool := tools[0].(map[string]any)

	if tool["name"] != "echo" || tool["title"] != "Echo" {
		t.Errorf("the tool is listed as %v", tool)
	}
	annotations := tool["annotations"].(map[string]any)
	if annotations["readOnlyHint"] != true || annotations["destructiveHint"] != false {
		t.Errorf("the hints a client asks a person about are wrong: %v", annotations)
	}

	schema := tool["inputSchema"].(map[string]any)
	if schema["additionalProperties"] != false {
		t.Error("the schema accepts arguments the tool does not take")
	}
	required := schema["required"].([]any)
	if len(required) != 1 || required[0] != "say" {
		t.Errorf("required is %v, want [say]", required)
	}

	properties := schema["properties"].(map[string]any)
	if properties["times"].(map[string]any)["type"] != "integer" {
		t.Error("an integer argument is not described as one")
	}
	tags := properties["tags"].(map[string]any)
	if tags["type"] != "array" {
		t.Errorf("a list argument is described as %v", tags["type"])
	}
	items := tags["items"].(map[string]any)
	if items["type"] != "string" || len(items["enum"].([]any)) != 2 {
		t.Errorf("a list argument does not describe its items: %v", items)
	}
}

// A stream that stops mid-message is a client that went away, which is how
// every session ends. It is not a failure to report.
func TestAHalfWrittenMessageEndsTheSessionQuietly(t *testing.T) {
	var out strings.Builder
	server := testServer(echoTool(nil))
	if err := server.Serve(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n"+`{"jsonrpc":"2.0","id`), &out); err != nil {
		t.Fatalf("serving reported %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	// The complete message is answered; the truncated one is not valid JSON and
	// says so, which is all a parse error can do without an id to reply to.
	if len(lines) != 2 {
		t.Fatalf("got %d answers: %v", len(lines), lines)
	}
	if code := rpcFailure(t, lines[1])["code"]; code != float64(codeParse) {
		t.Errorf("a truncated message answered %v, want %d", code, codeParse)
	}
}

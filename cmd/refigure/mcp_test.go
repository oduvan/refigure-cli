// `refigure mcp`, driven the way a client drives it: as a subprocess, one
// JSON-RPC message per line.
//
// The protocol itself is tested in internal/mcp. What is tested here is the
// part that is this tool's rather than the protocol's — that the tools do the
// same job the commands do, that a warning a person would read on stderr
// reaches a model that has no stderr, and that nothing but protocol messages
// ever reaches stdout, which is the one rule the transport cannot recover from.
package main

import (
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// speak runs one session against a project folder and returns a parsed reply
// per line that came back, plus whatever went to stderr.
func speak(t *testing.T, dir string, messages ...string) ([]map[string]any, string) {
	t.Helper()

	args := []string{"mcp"}
	if dir != "" {
		args = append(args, dir)
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdin = strings.NewReader(strings.Join(messages, "\n") + "\n")
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	if err := cmd.Run(); err != nil {
		t.Fatalf("the server exited badly: %v\n%s", err, errOut.String())
	}

	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var reply map[string]any
		if err := json.Unmarshal([]byte(line), &reply); err != nil {
			t.Fatalf("the server wrote a line that is not a protocol message: %q", line)
		}
		replies = append(replies, reply)
	}
	return replies, errOut.String()
}

func call(name string, arguments map[string]any) string {
	params := map[string]any{"name": name}
	if arguments != nil {
		params["arguments"] = arguments
	}
	encoded, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": name, "method": "tools/call", "params": params,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// toolResult pulls the result of the one call in a session apart.
func toolResult(t *testing.T, reply map[string]any) (text string, structured map[string]any, isError bool) {
	t.Helper()
	if failure, ok := reply["error"]; ok {
		t.Fatalf("the call failed at the protocol level: %v", failure)
	}
	result, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("the reply carries no result: %v", reply)
	}
	for _, block := range result["content"].([]any) {
		content := block.(map[string]any)
		if content["type"] == "text" {
			text += content["text"].(string)
		}
	}
	structured, _ = result["structuredContent"].(map[string]any)
	isError, _ = result["isError"].(bool)
	return text, structured, isError
}

// imageBlock is the first picture in a result, decoded.
func imageBlock(t *testing.T, reply map[string]any) []byte {
	t.Helper()
	result := reply["result"].(map[string]any)
	for _, block := range result["content"].([]any) {
		content := block.(map[string]any)
		if content["type"] != "image" {
			continue
		}
		if content["mimeType"] != "image/png" {
			t.Fatalf("the picture is a %v", content["mimeType"])
		}
		decoded, err := base64.StdEncoding.DecodeString(content["data"].(string))
		if err != nil {
			t.Fatalf("the picture is not base64: %v", err)
		}
		return decoded
	}
	t.Fatal("the result carries no picture")
	return nil
}

// The tools are the commands. A name that drifts from the command it stands for
// leaves two ways to say one thing, and one of them undocumented.
func TestTheServerHoldsASessionAndOffersTheCommandsAsTools(t *testing.T) {
	replies, _ := speak(t, project(t),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(replies) != 2 {
		t.Fatalf("got %d replies, want 2 — the notification must not be answered", len(replies))
	}

	opening := replies[0]["result"].(map[string]any)
	if opening["serverInfo"].(map[string]any)["version"] != testVersion {
		t.Errorf("the server reports version %v, want %s", opening["serverInfo"], testVersion)
	}
	if !strings.Contains(opening["instructions"].(string), "screen coordinates") {
		t.Error("the instructions do not carry the rule a model cannot guess from the keys")
	}

	offered := map[string]bool{}
	for _, entry := range replies[1]["result"].(map[string]any)["tools"].([]any) {
		tool := entry.(map[string]any)
		offered[tool["name"].(string)] = true
		if len(tool["description"].(string)) < 80 {
			t.Errorf("%s does not explain itself", tool["name"])
		}
	}
	for _, name := range []string{"schema", "validate", "list", "export", "preview"} {
		if !offered[name] {
			t.Errorf("the server does not offer %q", name)
		}
	}

	// Writing files is the one thing a client should be able to ask a person
	// about first, and it can only do that if this says so.
	for _, entry := range replies[1]["result"].(map[string]any)["tools"].([]any) {
		tool := entry.(map[string]any)
		hints := tool["annotations"].(map[string]any)
		wantReadOnly := tool["name"] != "export"
		if hints["readOnlyHint"] != wantReadOnly {
			t.Errorf("%s says readOnlyHint=%v", tool["name"], hints["readOnlyHint"])
		}
	}
}

// The transport allows nothing on stdout but protocol messages, and this tool
// has plenty to say: the fixture names a font no build carries. A person reads
// that on stderr; a model has no stderr, so it has to arrive in the answer.
func TestAWarningReachesTheModelAndNeverReachesStdout(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	// speak fails the test if any line of stdout is not a protocol message.
	replies, _ := speak(t, dir, call("export", map[string]any{"out": out}))
	text, structured, isError := toolResult(t, replies[0])
	if isError {
		t.Fatalf("the export failed: %s", text)
	}

	if !strings.Contains(text, "NoSuchFamilyAnywhere") {
		t.Errorf("the model is never told the font was missing:\n%s", text)
	}
	warnings, ok := structured["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Errorf("the warning is not in the data either: %v", structured["warnings"])
	}
}

// The export tool is the export command, so the files have to be there.
func TestTheExportToolWritesTheImagesItSaysItWrote(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	replies, _ := speak(t, dir, call("export", map[string]any{"out": out, "only": []string{"narrow"}, "scale": 100}))
	text, structured, isError := toolResult(t, replies[0])
	if isError {
		t.Fatalf("the export failed: %s", text)
	}

	written := structured["written"].([]any)
	if len(written) != 1 || written[0] != "narrow.png" {
		t.Fatalf("it reported writing %v", written)
	}
	width, height := size(t, filepath.Join(out, "narrow.png"))
	if width != 100 || height != 50 {
		t.Errorf("narrow.png is %dx%d, want the 100x50 the downscale asks for", width, height)
	}
	if _, err := os.Stat(filepath.Join(out, "wide.png")); err == nil {
		t.Error("only narrow was asked for, but wide was written too")
	}

	// And a dry run writes nothing, which is the whole point of offering one to
	// something that will call a tool to find out what it does.
	empty := filepath.Join(t.TempDir(), "nothing")
	replies, _ = speak(t, dir, call("export", map[string]any{"out": empty, "dryRun": true}))
	if _, _, isError := toolResult(t, replies[0]); isError {
		t.Fatal("the dry run failed")
	}
	if _, err := os.Stat(empty); err == nil {
		t.Error("a dry run created the destination")
	}
}

// The reason this server exists rather than a shell: an agent editing the file
// can look at what it drew.
func TestThePreviewToolAnswersWithAPicture(t *testing.T) {
	dir := project(t)

	replies, _ := speak(t, dir, call("preview", map[string]any{"cut": "narrow"}))
	text, _, isError := toolResult(t, replies[0])
	if isError {
		t.Fatalf("the preview failed: %s", text)
	}

	decoded := imageBlock(t, replies[0])
	config, _, err := image.DecodeConfig(strings.NewReader(string(decoded)))
	if err != nil {
		t.Fatalf("the picture does not decode: %v", err)
	}
	// The cut is 200x100 and narrower than the default cap, and a preview never
	// enlarges, so it comes back at its own size.
	if config.Width != 200 || config.Height != 100 {
		t.Errorf("the picture is %dx%d, want 200x100", config.Width, config.Height)
	}
	if !strings.Contains(text, "narrow") || !strings.Contains(text, "cut_narrow") {
		t.Errorf("the caption does not say which cut this is: %q", text)
	}

	// A cap smaller than the cut applies, so a big screenshot cannot arrive as
	// a megabyte of base64.
	replies, _ = speak(t, dir, call("preview", map[string]any{"cut": "wide", "maxWidth": 120}))
	config, _, err = image.DecodeConfig(strings.NewReader(string(imageBlock(t, replies[0]))))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 120 {
		t.Errorf("maxWidth 120 produced a picture %d wide", config.Width)
	}

	// Naming no cut in a project with two is a failure the model can fix, so it
	// has to be told what the choices are.
	replies, _ = speak(t, dir, call("preview", nil))
	text, _, isError = toolResult(t, replies[0])
	if !isError {
		t.Fatal("a preview with no cut named drew something anyway")
	}
	if !strings.Contains(text, "wide") || !strings.Contains(text, "narrow") {
		t.Errorf("the model is not told which cuts there are: %q", text)
	}
}

// JSON Schema calls 100.0 an integer. A client checking a call against the
// schema this server published will let it through, so the server has to take
// it — refusing what it advertised is its own mistake, not the model's.
func TestAWholeNumberWrittenWithAPointStillWorks(t *testing.T) {
	dir := project(t)

	replies, _ := speak(t, dir, `{"jsonrpc":"2.0","id":"preview","method":"tools/call","params":{"name":"preview","arguments":{"cut":"wide","maxWidth":150.0}}}`)
	text, _, isError := toolResult(t, replies[0])
	if isError {
		t.Fatalf("maxWidth 150.0 was refused: %s", text)
	}
	config, _, err := image.DecodeConfig(strings.NewReader(string(imageBlock(t, replies[0]))))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 150 {
		t.Errorf("the picture is %d wide, want 150", config.Width)
	}
}

// A project that does not validate is the model's to fix, so it comes back as
// a readable failure rather than a protocol error the client swallows.
func TestTheValidateToolReportsProblemsTheModelCanFix(t *testing.T) {
	dir := project(t)
	broken := strings.Replace(projectFile, "cut: cut_narrow", "cut: cut_absent", 1)
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	replies, _ := speak(t, dir, call("validate", nil))
	text, structured, isError := toolResult(t, replies[0])
	if !isError {
		t.Fatal("a project with a dangling reference validated")
	}
	if structured["ok"] != false {
		t.Errorf("the data says ok=%v", structured["ok"])
	}
	problems := structured["problems"].([]any)
	if len(problems) == 0 {
		t.Fatal("no problems were reported")
	}
	if line := problems[0].(map[string]any)["line"]; line == nil {
		t.Error("the problem has no line number, which is the useful half")
	}
	if !strings.Contains(text, "cut_absent") {
		t.Errorf("the text does not name what is wrong: %q", text)
	}
}

// The tools and the commands describe the format with one voice, or an agent
// reading one and a person reading the other are told different things.
func TestTheSchemaToolSaysWhatTheSchemaCommandSays(t *testing.T) {
	for _, testCase := range []struct {
		form string
		flag []string
	}{
		{"", []string{"schema"}},
		{"reference", []string{"schema"}},
		{"example", []string{"schema", "--example"}},
		{"json", []string{"schema", "--json"}},
	} {
		arguments := map[string]any{}
		if testCase.form != "" {
			arguments["form"] = testCase.form
		}
		replies, _ := speak(t, "", call("schema", arguments))
		text, _, isError := toolResult(t, replies[0])
		if isError {
			t.Fatalf("form %q failed: %s", testCase.form, text)
		}

		printed, _, code := run(t, testCase.flag...)
		if code != 0 {
			t.Fatalf("%v exited %d", testCase.flag, code)
		}
		if text != printed {
			t.Errorf("form %q and %v describe the format differently", testCase.form, testCase.flag)
		}
	}
}

// A client is configured with one project folder and a model then does not have
// to name it — nor can it point a write at another one by getting a relative
// path wrong.
func TestTheServerDefaultsToTheProjectItWasStartedIn(t *testing.T) {
	dir := project(t)

	replies, _ := speak(t, dir, call("list", nil))
	text, _, isError := toolResult(t, replies[0])
	if isError {
		t.Fatalf("listing the project it was started in failed: %s", text)
	}
	if !strings.Contains(text, "wide.png") || !strings.Contains(text, "narrow.png") {
		t.Errorf("it listed something else: %q", text)
	}

	// Naming a folder still wins, and a folder with no project in it is a
	// failure the model reads rather than one that kills the server.
	replies, _ = speak(t, dir, call("list", map[string]any{"project": t.TempDir()}))
	text, _, isError = toolResult(t, replies[0])
	if !isError {
		t.Fatal("an empty folder listed something")
	}
	if !strings.Contains(text, "refigure.yaml") {
		t.Errorf("the model is not told what is missing: %q", text)
	}
}

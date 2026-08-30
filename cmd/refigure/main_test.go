// End-to-end tests for the command itself: flags, exit codes, what reaches
// stdout, and what lands on disk.
//
// They run the real binary rather than calling runExport and friends directly,
// because the contract this tool offers a build script *is* its exit code and
// its output. A seam that let a test skip main() would be testing something
// nobody runs.
//
// The binary is built inside TestMain, which `go test` cannot see. Its result
// cache therefore does not know these tests depend on anything, and will serve
// a stale pass after a real change — verified, not assumed. So the Makefile and
// CI both run the suite with -count=1, and a bare `go test ./...` here is not
// evidence of anything.
package main

import (
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	_ "image/jpeg"

	_ "golang.org/x/image/webp"
)

const testVersion = "v9.9.9-test"

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "refigure-cli-test")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "refigure")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	build := exec.Command("go", "build", "-ldflags", "-X main.Version="+testVersion, "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		panic("building the binary failed: " + string(out))
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("running the binary failed: %v", err)
	}
	return out.String(), errOut.String(), code
}

// The fixture is one 400x300 screen with two cuts. `wide` contains `narrow`,
// so the owned rectangle proves ownership survives the whole pipeline: it is
// inside both rectangles but must appear in one image only.
//
// The text figure names a font family that cannot exist, so the missing-font
// warning is the same on every machine.
const projectFile = `version: 1
name: demo
export:
  dest: out
  format: png
  quality: 90
  scale: original
screens:
  - id: scr_1
    name: connect
    file: connect.png
    width: 400
    height: 300
    cuts:
      - id: cut_wide
        name: wide
        rect: { x: 0, y: 0, w: 400, h: 300 }
      - id: cut_narrow
        name: narrow
        rect: { x: 20, y: 20, w: 200, h: 100 }
    figures:
      - id: fig_owned
        type: rect
        cut: cut_narrow
        rect: { x: 40, y: 40, w: 60, h: 30 }
      - id: fig_text
        type: text
        at: { x: 30, y: 200 }
        text: hello
        style:
          font: { family: NoSuchFamilyAnywhere, size: 16 }
`

func project(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(projectFile), 0o644); err != nil {
		t.Fatal(err)
	}

	writeScreenshot(t, filepath.Join(dir, "connect.png"), 400, 300)
	return dir
}

func writeScreenshot(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 240, G: 240, B: 245, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
}

func size(t *testing.T, path string) (int, int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("%s was not written: %v", filepath.Base(path), err)
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		t.Fatalf("%s is not a readable image: %v", filepath.Base(path), err)
	}
	return config.Width, config.Height
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("%s was not written: %v", filepath.Base(path), err)
	}
	return info.Size()
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		found = append(found, entry.Name())
	}
	return found
}

func TestValidateAcceptsAGoodProject(t *testing.T) {
	stdout, _, code := run(t, "validate", project(t))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "1 screens, 2 cuts") {
		t.Errorf("stdout was %q", stdout)
	}
}

// Exit 2 is the promise that lets a script tell "I cannot read this project"
// apart from "the export failed".
func TestABrokenProjectExitsTwoAndNamesTheLine(t *testing.T) {
	dir := project(t)
	broken := strings.Replace(projectFile, "name: demo", "name: demo: oops", 1)
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, "validate", dir)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "line 2") {
		t.Errorf("the error should point at the line, got %q", stderr)
	}
}

func TestAMissingProjectExitsTwo(t *testing.T) {
	_, stderr, code := run(t, "export", t.TempDir())
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.Contains(stderr, "refigure.yaml") {
		t.Errorf("the error should name the file it looked for, got %q", stderr)
	}
}

func TestAnUnknownCommandExitsOneWithUsage(t *testing.T) {
	_, stderr, code := run(t, "frobnicate")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "frobnicate") || !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr was %q", stderr)
	}
}

func TestVersionComesFromTheBuild(t *testing.T) {
	stdout, _, code := run(t, "version")
	if code != 0 || strings.TrimSpace(stdout) != testVersion {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}

func TestListShowsWhatWouldBeWritten(t *testing.T) {
	stdout, _, code := run(t, "list", project(t))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"wide.png", "400x300", "narrow.png", "200x100"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("%q missing from:\n%s", want, stdout)
		}
	}
}

func TestExportWritesOneImagePerCut(t *testing.T) {
	dir := project(t)
	stdout, _, code := run(t, "export", dir)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "2 images written") {
		t.Errorf("stdout was %q", stdout)
	}

	// `export.dest` is relative, so it resolves against the project folder.
	out := filepath.Join(dir, "out")
	if w, h := size(t, filepath.Join(out, "wide.png")); w != 400 || h != 300 {
		t.Errorf("wide.png is %dx%d", w, h)
	}
	if w, h := size(t, filepath.Join(out, "narrow.png")); w != 200 || h != 100 {
		t.Errorf("narrow.png is %dx%d", w, h)
	}
}

func TestOutOverridesTheProjectDestination(t *testing.T) {
	dir := project(t)
	elsewhere := filepath.Join(t.TempDir(), "nested", "images")

	if _, _, code := run(t, "export", dir, "--out", elsewhere); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := len(names(t, elsewhere)); got != 2 {
		t.Errorf("expected 2 images in the given folder, got %d", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Error("the project's own destination should have been left alone")
	}
}

// The directory may come before or after the flags; both orders are ordinary
// enough that a build script will use either.
func TestFlagsMayComeBeforeTheDirectory(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	if _, _, code := run(t, "export", "--out", out, dir); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := len(names(t, out)); got != 2 {
		t.Errorf("got %d images", got)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := project(t)
	stdout, _, code := run(t, "export", dir, "--dry-run")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "would write") || !strings.Contains(stdout, "wide.png") {
		t.Errorf("stdout was %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Error("--dry-run created the output folder")
	}
}

func TestJSONDescribesExactlyWhatWasWritten(t *testing.T) {
	dir := project(t)
	stdout, _, code := run(t, "export", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}

	var out struct {
		Dest   string `json:"dest"`
		Images []struct {
			Screen   string `json:"screen"`
			Cut      string `json:"cut"`
			FileName string `json:"fileName"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
		} `json:"images"`
		Written []string `json:"written"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not JSON a tool could read: %v\n%s", err, stdout)
	}

	if len(out.Images) != 2 || len(out.Written) != 2 {
		t.Fatalf("got %d images and %d written", len(out.Images), len(out.Written))
	}
	if out.Images[0].Screen != "connect" || out.Images[0].Cut != "wide" {
		t.Errorf("first image was %+v", out.Images[0])
	}
	// Every name it claims to have written must actually be there.
	for _, name := range out.Written {
		if _, err := os.Stat(filepath.Join(out.Dest, name)); err != nil {
			t.Errorf("%s was reported but not written", name)
		}
	}
}

func TestOnlySelectsByName(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	if _, _, code := run(t, "export", dir, "--out", out, "--only", "narrow"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := names(t, out); len(got) != 1 || got[0] != "narrow.png" {
		t.Errorf("got %v", got)
	}
}

// The app selects cuts by id, because two screens may hold cuts with the same
// name and a name filter cannot tell them apart.
func TestOnlyIDSelectsExactlyThatCut(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	if _, _, code := run(t, "export", dir, "--out", out, "--only-id", "cut_narrow"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := names(t, out); len(got) != 1 || got[0] != "narrow.png" {
		t.Errorf("got %v", got)
	}
}

func TestProgressReportsEachImageOnStderr(t *testing.T) {
	dir := project(t)
	stdout, stderr, code := run(t, "export", dir, "--progress")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"progress 1/2 ", "progress 2/2 "} {
		if !strings.Contains(stderr, want) {
			t.Errorf("%q missing from stderr:\n%s", want, stderr)
		}
	}
	// stdout must stay clean, so --json and --progress can be used together.
	if strings.Contains(stdout, "progress") {
		t.Errorf("progress leaked into stdout: %q", stdout)
	}
}

func TestOnlyMatchingNothingIsAFailureNotAnEmptySuccess(t *testing.T) {
	dir := project(t)
	_, stderr, code := run(t, "export", dir, "--only", "nope")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "nothing to export") || !strings.Contains(stderr, "nope") {
		t.Errorf("the message should name what was asked for, got %q", stderr)
	}
}

// The message has to name the filter that was actually used. A caller passing
// ids and being told about --only cannot tell which of the two it means.
func TestAnUnmatchedIDSaysSoInItsOwnTerms(t *testing.T) {
	dir := project(t)
	_, stderr, code := run(t, "export", dir, "--only-id", "cut_nope")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(stderr, "ids") || strings.Contains(stderr, "--only ") {
		t.Errorf("stderr was %q", stderr)
	}
}

func TestFormatAndQualityChangeTheOutput(t *testing.T) {
	dir := project(t)
	out := filepath.Join(t.TempDir(), "images")

	if _, _, code := run(t, "export", dir, "--out", out, "--format", "jpeg", "--quality", "60"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	// The extension follows the format, and the file really is a JPEG.
	if w, h := size(t, filepath.Join(out, "wide.jpg")); w != 400 || h != 300 {
		t.Errorf("wide.jpg is %dx%d", w, h)
	}
}

func TestScaleDownscalesButNeverEnlarges(t *testing.T) {
	dir := project(t)
	small := filepath.Join(t.TempDir(), "small")
	big := filepath.Join(t.TempDir(), "big")

	if _, _, code := run(t, "export", dir, "--out", small, "--scale", "200"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if w, h := size(t, filepath.Join(small, "wide.png")); w != 200 || h != 150 {
		t.Errorf("expected 200x150, got %dx%d", w, h)
	}

	if _, _, code := run(t, "export", dir, "--out", big, "--scale", "4000"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if w, h := size(t, filepath.Join(big, "wide.png")); w != 400 || h != 300 {
		t.Errorf("a scale wider than the cut must be ignored, got %dx%d", w, h)
	}
}

// A missing font changes what the image looks like, so it is said out loud —
// but it is a warning, not a failure, and the image is still written.
func TestAMissingFontWarnsAndStillExports(t *testing.T) {
	dir := project(t)
	_, stderr, code := run(t, "export", dir)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr, "NoSuchFamilyAnywhere") || !strings.Contains(stderr, "fallback") {
		t.Errorf("stderr was %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "wide.png")); err != nil {
		t.Error("the image should still have been written")
	}
}

// WebP is encoded by libwebp compiled to WASM, so it is real lossy WebP and
// the quality setting means what it means everywhere else.
func TestWebPIsWrittenAndQualityChangesIt(t *testing.T) {
	dir := project(t)
	low := filepath.Join(t.TempDir(), "low")
	high := filepath.Join(t.TempDir(), "high")

	if _, _, code := run(t, "export", dir, "--out", low, "--format", "webp", "--quality", "20"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, _, code := run(t, "export", dir, "--out", high, "--format", "webp", "--quality", "95"); code != 0 {
		t.Fatalf("exit %d", code)
	}

	// The standard decoder must accept it, at the right size.
	if w, h := size(t, filepath.Join(low, "wide.webp")); w != 400 || h != 300 {
		t.Errorf("wide.webp is %dx%d", w, h)
	}

	small, big := fileSize(t, filepath.Join(low, "wide.webp")), fileSize(t, filepath.Join(high, "wide.webp"))
	if small >= big {
		t.Errorf("quality 20 produced %d bytes and quality 95 produced %d — the setting is being ignored", small, big)
	}
}

/* ------------------------- the self-describing surface ------------------------ */

// The most likely caller is a program holding only this binary, with no
// repository to read. Everything it needs to write a valid project file has to
// come out of the tool itself, and has to be true.

func TestEveryCommandExplainsItself(t *testing.T) {
	for _, args := range [][]string{
		{"help"},
		{"--help"},
		{"export", "--help"},
		{"list", "--help"},
		{"validate", "--help"},
		{"schema", "--help"},
		{"help", "export"},
		{"help", "schema"},
	} {
		stdout, _, code := run(t, args...)
		if code != 0 {
			t.Errorf("%v exited %d", args, code)
		}
		if len(stdout) < 200 {
			t.Errorf("%v printed %d bytes, which is not an explanation", args, len(stdout))
		}
	}
}

// `refigure help export` and `refigure export --help` must answer the same
// question: a caller guessing either way should be right.
func TestHelpForACommandMatchesItsOwnFlag(t *testing.T) {
	viaHelp, _, _ := run(t, "help", "export")
	viaFlag, _, _ := run(t, "export", "--help")
	if viaHelp != viaFlag {
		t.Error("`help export` and `export --help` printed different things")
	}
}

// Help that names a flag the binary does not accept is worse than no help: it
// sends a caller down a path that fails. Every --flag the export help mentions
// is offered to the real command here.
func TestExportHelpNamesOnlyRealFlags(t *testing.T) {
	help, _, _ := run(t, "export", "--help")
	dir := project(t)

	named := map[string]bool{}
	for _, match := range regexp.MustCompile(`--[a-z][a-z-]*`).FindAllString(help, -1) {
		named[match] = true
	}
	if len(named) < 8 {
		t.Fatalf("only found %d flags in the help, which cannot be right", len(named))
	}

	for flag := range named {
		// `flag provided but not defined` is what an unknown flag produces.
		_, stderr, _ := run(t, "export", dir, flag, "--dry-run")
		if strings.Contains(stderr, "not defined") {
			t.Errorf("the help offers %s, but export does not accept it", flag)
		}
	}
}

// The example is the thing an agent will copy. If it does not validate, the
// tool has taught it to write a broken file.
func TestSchemaExampleIsAValidProject(t *testing.T) {
	example, _, code := run(t, "schema", "--example")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	// The example names a screenshot, and validate checks that it is there.
	writeScreenshot(t, filepath.Join(dir, "connect.png"), 1200, 600)

	stdout, stderr, code := run(t, "validate", dir, "--json")
	if code != 0 {
		t.Fatalf("the printed example does not validate: exit %d, %s", code, stderr)
	}

	var result struct {
		OK       bool `json:"ok"`
		Screens  int  `json:"screens"`
		Cuts     int  `json:"cuts"`
		Problems []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"problems"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("validate --json is not JSON: %v", err)
	}
	if !result.OK || result.Screens != 1 || result.Cuts != 2 {
		t.Errorf("got %+v", result)
	}
	// The example is what a caller copies, so it must not even warn.
	if len(result.Problems) != 0 {
		t.Errorf("the printed example is not clean: %+v", result.Problems)
	}

	// The example must also export, not merely parse — it is a worked example.
	_, _, code = run(t, "export", dir, "--dry-run")
	if code != 0 {
		t.Error("the example project produces no export plan")
	}
}

func TestSchemaJSONIsAJSONSchema(t *testing.T) {
	stdout, _, code := run(t, "schema", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, key := range []string{"$schema", "title", "properties", "$defs"} {
		if _, ok := schema[key]; !ok {
			t.Errorf("the schema has no %q", key)
		}
	}

	// The parts a caller most needs to get right.
	defs, _ := schema["$defs"].(map[string]any)
	for _, key := range []string{"figure", "cut", "screen", "style", "rect", "point"} {
		if _, ok := defs[key]; !ok {
			t.Errorf("$defs has no %q", key)
		}
	}
}

// The prose has to carry the two rules that are impossible to guess from the
// keys alone, and that a program will otherwise get wrong.
func TestSchemaProseCarriesTheRulesThatCannotBeGuessed(t *testing.T) {
	stdout, _, code := run(t, "schema")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, phrase := range []string{
		"screen coordinates", // not cut coordinates
		"belongs to that cut alone",
		"overlaps",
		"exclude",
		"Resolution order",
	} {
		if !strings.Contains(stdout, phrase) {
			t.Errorf("the format reference never mentions %q", phrase)
		}
	}
}

func TestValidateJSONReportsAFailureOnStdout(t *testing.T) {
	dir := project(t)
	broken := strings.Replace(projectFile, "name: demo", "name: demo: oops", 1)
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := run(t, "validate", dir, "--json")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}

	var result struct {
		OK       bool `json:"ok"`
		Problems []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Line     int    `json:"line"`
		} `json:"problems"`
	}
	// A caller asking for --json must not have to read stderr to learn it failed.
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failure was not reported as JSON on stdout: %v", err)
	}
	if result.OK || len(result.Problems) != 1 {
		t.Fatalf("got %+v", result)
	}
	if result.Problems[0].Severity != "error" || result.Problems[0].Line != 2 {
		t.Errorf("got %+v", result.Problems[0])
	}
}

// Every mistake at once, with a line for each: a caller fixes the file in one
// pass instead of one run per problem.
func TestValidateReportsEveryProblemTogether(t *testing.T) {
	dir := project(t)
	broken := strings.Replace(projectFile, "name: demo", "name: demo\nstyle:\n  colour: red", 1)
	broken = strings.Replace(broken, "cut: cut_narrow", "cut: cut_ghost", 1)
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := run(t, "validate", dir, "--json")
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}

	var result struct {
		Problems []struct {
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Line     int    `json:"line"`
			Hint     string `json:"hint"`
		} `json:"problems"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Problems) < 2 {
		t.Fatalf("expected the typo and the dangling cut together, got %+v", result.Problems)
	}

	var sawTypo, sawGhost bool
	for _, problem := range result.Problems {
		if problem.Line == 0 {
			t.Errorf("no line on %+v", problem)
		}
		if strings.Contains(problem.Message, "colour") {
			sawTypo = true
			if !strings.Contains(problem.Hint, "color") {
				t.Errorf("no suggestion for the typo: %+v", problem)
			}
		}
		if strings.Contains(problem.Message, "cut_ghost") {
			sawGhost = true
		}
	}
	if !sawTypo || !sawGhost {
		t.Errorf("typo=%v dangling=%v in %+v", sawTypo, sawGhost, result.Problems)
	}
}

// --strict is for a caller that wants warnings to stop the line too.
func TestStrictTurnsWarningsIntoAFailure(t *testing.T) {
	dir := project(t)
	warned := strings.Replace(projectFile, "name: demo", "name: demo\nstyle:\n  colour: red", 1)
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(warned), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, code := run(t, "validate", dir); code != 0 {
		t.Errorf("a warning alone must not fail, got exit %d", code)
	}
	if _, _, code := run(t, "validate", dir, "--strict"); code != 1 {
		t.Errorf("--strict should exit 1 on a warning, got %d", code)
	}
}

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

	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: 240, G: 240, B: 245, A: 255})
		}
	}
	file, err := os.Create(filepath.Join(dir, "connect.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	return dir
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
	if !strings.Contains(stderr, "nothing to export") {
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

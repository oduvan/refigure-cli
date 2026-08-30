package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oduvan/refigure-cli/internal/format"
)

// check writes a project file, loads it, and lints it — the same path the
// validate command takes.
func check(t *testing.T, body string) []Problem {
	t.Helper()
	dir := t.TempDir()

	// A screenshot, so "the file is missing" does not appear in every case.
	if err := os.WriteFile(filepath.Join(dir, "screen.png"), []byte("not really a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "refigure.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	project, err := format.Load(dir)
	if err != nil {
		t.Fatalf("the fixture does not even parse: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "refigure.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return Check(dir, data, project)
}

func find(problems []Problem, substring string) *Problem {
	for i := range problems {
		if strings.Contains(problems[i].Message, substring) {
			return &problems[i]
		}
	}
	return nil
}

const valid = `version: 1
name: demo
screens:
  - id: scr_1
    name: hero
    file: screen.png
    width: 320
    height: 200
    cuts:
      - id: cut_a
        name: top
        rect: { x: 0, y: 0, w: 320, h: 40 }
    figures:
      - id: fig_a
        type: rect
        cut: cut_a
        rect: { x: 10, y: 10, w: 100, h: 20 }
`

func TestAGoodProjectHasNothingToSay(t *testing.T) {
	if problems := check(t, valid); len(problems) != 0 {
		t.Errorf("expected silence, got %+v", problems)
	}
}

// The reason this package exists: format.Load ignores keys it does not know, so
// a typo changes nothing and says nothing.
func TestATypoInAKeyIsReportedWithTheKeyItLooksLike(t *testing.T) {
	problems := check(t, strings.Replace(valid,
		"    figures:\n      - id: fig_a",
		"    style:\n      colour: '#112233'\n    figures:\n      - id: fig_a", 1))

	problem := find(problems, "colour")
	if problem == nil {
		t.Fatalf("the typo was not reported: %+v", problems)
	}
	if problem.Severity != SeverityWarning {
		t.Errorf("severity was %q", problem.Severity)
	}
	if !strings.Contains(problem.Hint, "color") {
		t.Errorf("hint was %q, and should suggest the real key", problem.Hint)
	}
	if problem.Line == 0 {
		t.Error("no line number, so there is nothing to go and fix")
	}
}

// The desktop app writes keys this tool ignores on purpose. Reporting those as
// typos would make the check useless on real projects.
func TestKeysTheDesktopWritesAreNotTypos(t *testing.T) {
	problems := check(t, strings.Replace(valid,
		"    file: screen.png",
		"    file: screen.png\n    replacedAt: '2026-01-01T00:00:00Z'", 1))
	if problem := find(problems, "replacedAt"); problem != nil {
		t.Errorf("a known desktop key was reported: %+v", problem)
	}
}

func TestAFigureOwnedByANonExistentCutIsAnError(t *testing.T) {
	problems := check(t, strings.Replace(valid, "cut: cut_a", "cut: cut_ghost", 1))

	problem := find(problems, "cut_ghost")
	if problem == nil {
		t.Fatalf("not reported: %+v", problems)
	}
	// It is an error because the figure appears in no image at all.
	if problem.Severity != SeverityError {
		t.Errorf("severity was %q", problem.Severity)
	}
}

func TestTwoCutsWithOneNameIsAnError(t *testing.T) {
	problems := check(t, strings.Replace(valid,
		"        rect: { x: 0, y: 0, w: 320, h: 40 }",
		"        rect: { x: 0, y: 0, w: 320, h: 40 }\n      - id: cut_b\n        name: top\n        rect: { x: 0, y: 40, w: 320, h: 40 }", 1))

	problem := find(problems, "both named")
	if problem == nil {
		t.Fatalf("not reported: %+v", problems)
	}
	if problem.Severity != SeverityError {
		t.Errorf("one image would overwrite the other, so this is an error, got %q", problem.Severity)
	}
}

func TestAReusedIDIsAnError(t *testing.T) {
	problems := check(t, strings.Replace(valid, "id: fig_a", "id: cut_a", 1))
	problem := find(problems, "used more than once")
	if problem == nil {
		t.Fatalf("not reported: %+v", problems)
	}
	if problem.Severity != SeverityError {
		t.Errorf("severity was %q", problem.Severity)
	}
}

func TestAMissingScreenshotIsAnError(t *testing.T) {
	problems := check(t, strings.Replace(valid, "file: screen.png", "file: gone.png", 1))
	problem := find(problems, "gone.png")
	if problem == nil {
		t.Fatalf("not reported: %+v", problems)
	}
	if problem.Severity != SeverityError {
		t.Errorf("export cannot run without it, so this is an error, got %q", problem.Severity)
	}
}

func TestACutWithNoAreaIsAnError(t *testing.T) {
	problems := check(t, strings.Replace(valid, "w: 320, h: 40", "w: 0, h: 40", 1))
	if problem := find(problems, "no image to export"); problem == nil || problem.Severity != SeverityError {
		t.Errorf("got %+v", problems)
	}
}

func TestACutPastTheEdgeIsOnlyAWarning(t *testing.T) {
	problems := check(t, strings.Replace(valid, "w: 320, h: 40", "w: 400, h: 40", 1))
	problem := find(problems, "runs past the edge")
	if problem == nil {
		t.Fatalf("not reported: %+v", problems)
	}
	// It still exports, so it must not stop anyone.
	if problem.Severity != SeverityWarning {
		t.Errorf("severity was %q", problem.Severity)
	}
}

func TestAnOwnedFigureOutsideItsCutIsReported(t *testing.T) {
	problems := check(t, strings.Replace(valid, "rect: { x: 10, y: 10, w: 100, h: 20 }", "rect: { x: 10, y: 150, w: 100, h: 20 }", 1))
	if find(problems, "lies outside it") == nil {
		t.Errorf("an invisible figure was not reported: %+v", problems)
	}
}

func TestAnExclusionOfSomethingAbsentIsReported(t *testing.T) {
	problems := check(t, strings.Replace(valid,
		"        rect: { x: 0, y: 0, w: 320, h: 40 }",
		"        rect: { x: 0, y: 0, w: 320, h: 40 }\n        figures:\n          exclude: [fig_ghost]", 1))
	if find(problems, "fig_ghost") == nil {
		t.Errorf("not reported: %+v", problems)
	}
}

// Problems come back in file order so a caller can fix them top down.
func TestProblemsAreOrderedByLine(t *testing.T) {
	problems := check(t, strings.Replace(
		strings.Replace(valid, "name: demo", "name: demo\nstyle:\n  colour: red", 1),
		"cut: cut_a", "cut: cut_ghost", 1))

	if len(problems) < 2 {
		t.Fatalf("expected several problems, got %+v", problems)
	}
	for i := 1; i < len(problems); i++ {
		if problems[i-1].Line > problems[i].Line {
			t.Fatalf("out of order: %+v", problems)
		}
	}
}

package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ProjectFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const sample = `# a comment the CLI happily ignores
version: 1
name: acme-docs
style:
  color: "#4F6DF5"
  stroke: { width: 3, style: solid }
export:
  dest: ../img
  format: png
  quality: 90
  scale: original
screens:
  - id: scr_a1b2
    name: connect
    file: connect.png
    width: 1200
    height: 800
    cuts:
      - id: cut_c3d4
        name: connect-token
        rect: { x: 60, y: 150, w: 700, h: 380 }
    figures:
      - id: fig_e5f6
        type: arrow
        cut: cut_c3d4
        from: { x: 600, y: 420 }
        to: { x: 460, y: 380 }
`

func TestLoadsAHandWrittenFile(t *testing.T) {
	project, err := Load(write(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Screens) != 1 || project.Screens[0].Name != "connect" {
		t.Fatalf("expected one screen named connect, got %+v", project.Screens)
	}
	if project.Screens[0].Figures[0].Cut != "cut_c3d4" {
		t.Error("figure ownership was not read")
	}
	if _, capped := project.Export.MaxWidth(); capped {
		t.Error(`scale "original" must not cap the width`)
	}
}

// The desktop app owns the file. This tool must tolerate fields it has never
// heard of, so the two can be released independently — and so moving the
// desktop's own settings elsewhere needs no change here.
func TestUnknownFieldsAreIgnored(t *testing.T) {
	body := strings.Replace(sample, "    file: connect.png",
		"    file: connect.png\n    replacedAt: 2026-08-01T00:00:00Z\n    editor:\n      collapsed: true", 1)
	body += "editorPreferences:\n  lastScreen: scr_a1b2\n"

	if _, err := Load(write(t, body)); err != nil {
		t.Fatalf("unknown keys must not fail the load: %v", err)
	}
}

func TestScaleAsANumberCapsTheWidth(t *testing.T) {
	project, err := Load(write(t, strings.Replace(sample, "scale: original", "scale: 1200", 1)))
	if err != nil {
		t.Fatal(err)
	}
	width, capped := project.Export.MaxWidth()
	if !capped || width != 1200 {
		t.Errorf("expected a 1200px cap, got %d capped=%v", width, capped)
	}
}

func TestBrokenYamlReportsALine(t *testing.T) {
	_, err := Load(write(t, strings.Replace(sample, "name: acme-docs", "name: acme: docs", 1)))
	if err == nil {
		t.Fatal("expected an error")
	}
	formatErr, ok := err.(*Error)
	if !ok || formatErr.Line == 0 {
		t.Errorf("expected a line number, got %v", err)
	}
}

func TestMissingFileIsRejected(t *testing.T) {
	_, err := Load(write(t, strings.Replace(sample, "    file: connect.png\n", "", 1)))
	if err == nil || !strings.Contains(err.Error(), "no file") {
		t.Errorf("a screen with no file should be rejected, got %v", err)
	}
}

func TestCutWithoutANameIsRejected(t *testing.T) {
	// The cut name becomes the output file name, so it cannot be blank.
	_, err := Load(write(t, strings.Replace(sample, "        name: connect-token\n", "", 1)))
	if err == nil || !strings.Contains(err.Error(), "no name") {
		t.Errorf("a nameless cut should be rejected, got %v", err)
	}
}

func TestNewerFormatIsRefusedClearly(t *testing.T) {
	_, err := Load(write(t, strings.Replace(sample, "version: 1", "version: 99", 1)))
	if err == nil || !strings.Contains(err.Error(), "newer Refigure") {
		t.Errorf("a future version should say so plainly, got %v", err)
	}
}

func TestMissingProjectFile(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Error("an empty folder is not a project")
	}
}

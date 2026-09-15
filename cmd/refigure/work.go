// The work behind the commands, with none of the talking in it.
//
// `refigure export` and the `export` tool of `refigure mcp` do the same job for
// very different readers — one a person watching a terminal, one a model
// reading JSON. Keeping the job here and the reporting in the two callers is
// what stops them drifting into two slightly different exports.
package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/oduvan/refigure-cli/internal/export"
	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/lint"
	"github.com/oduvan/refigure-cli/internal/render"
)

// unreadableProject marks the one failure the command line reports as exit 2
// rather than 1, so a script can tell a broken project file from a failed
// write.
type unreadableProject struct{ err error }

func (e unreadableProject) Error() string { return e.err.Error() }
func (e unreadableProject) Unwrap() error { return e.err }

func openProject(dir string) (*format.Project, error) {
	project, err := format.Load(dir)
	if err != nil {
		return nil, unreadableProject{err}
	}
	return project, nil
}

// exportRequest is everything either caller can decide about an export.
type exportRequest struct {
	Dir      string
	Out      string
	Format   string
	Quality  int
	Scale    int
	Original bool
	Only     []string
	OnlyIDs  []string
	DryRun   bool
}

// exportOutcome is what an export did.
type exportOutcome struct {
	Dest    string
	DryRun  bool
	Plan    *export.Plan
	Written []string
}

// performExport renders and writes every image the request asks for.
//
// onWarn and onProgress are called as the work happens rather than gathered up
// and returned, because a person watching a slow export wants a warning when it
// is found. Either may be nil.
func performExport(req exportRequest, onWarn func(string), onProgress func(done, total int, fileName string)) (*exportOutcome, error) {
	if onWarn == nil {
		onWarn = func(string) {}
	}
	if onProgress == nil {
		onProgress = func(int, int, string) {}
	}

	project, err := openProject(req.Dir)
	if err != nil {
		return nil, err
	}

	opts := export.Options{
		Only:     req.Only,
		OnlyIDs:  req.OnlyIDs,
		Original: req.Original,
		MaxWidth: req.Scale,
	}
	if req.Format != "" {
		opts.Format = format.ExportFormat(req.Format)
	}

	plan, err := export.Build(project, opts)
	if err != nil {
		return nil, err
	}
	if len(plan.Items) == 0 {
		// Name whichever filter was actually used: a caller passing ids and
		// being told about --only has to go and read the flags to find out
		// which of the two it means.
		switch {
		case len(req.OnlyIDs) > 0:
			return nil, fmt.Errorf("nothing to export — no cut in this project has one of these ids: %s", strings.Join(req.OnlyIDs, ", "))
		case len(req.Only) > 0:
			return nil, fmt.Errorf("nothing to export — no cut or screen is named %q", strings.Join(req.Only, ", "))
		default:
			return nil, errors.New("nothing to export — the project has no cuts")
		}
	}

	dest := req.Out
	if dest == "" {
		dest = project.Export.Dest
	}
	if dest == "" {
		return nil, errors.New("no output directory — pass --out, or set `export.dest` in the project")
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(project.Dir, dest)
	}

	for _, name := range plan.Collisions {
		onWarn(fmt.Sprintf("two cuts are both named %q, so one image will overwrite the other",
			strings.TrimSuffix(name, filepath.Ext(name))))
	}

	outcome := &exportOutcome{Dest: dest, DryRun: req.DryRun, Plan: plan}
	if req.DryRun {
		return outcome, nil
	}

	if err := export.EnsureDir(dest); err != nil {
		return nil, err
	}

	outputFormat := project.Export.Format
	if opts.Format != "" {
		outputFormat = opts.Format
	}
	quality := project.Export.Quality
	if req.Quality > 0 {
		quality = req.Quality
	}

	screenshots := map[string]image.Image{}
	// Reported per export rather than per cut: a project drawn in a font this
	// build does not carry would otherwise say so once for every image.
	reportedFonts := map[string]bool{}
	for _, item := range plan.Items {
		img, err := renderItem(project, item, screenshots, reportedFonts, onWarn)
		if err != nil {
			return nil, err
		}
		if err := export.Encode(img, filepath.Join(dest, item.FileName), outputFormat, quality); err != nil {
			return nil, fmt.Errorf("cut %q: %w", item.Cut.Name, err)
		}
		outcome.Written = append(outcome.Written, item.FileName)
		onProgress(len(outcome.Written), len(plan.Items), item.FileName)
	}
	return outcome, nil
}

// renderItem draws one planned image at its final size. screenshots is the
// decoded picture per file, so a screen with four cuts is read once.
func renderItem(project *format.Project, item export.Item, screenshots map[string]image.Image, reportedFonts map[string]bool, onWarn func(string)) (image.Image, error) {
	screenshot, ok := screenshots[item.Screen.File]
	if !ok {
		loaded, err := loadImage(filepath.Join(project.Dir, item.Screen.File))
		if err != nil {
			return nil, fmt.Errorf("screen %q: %w", item.Screen.Name, err)
		}
		screenshots[item.Screen.File] = loaded
		screenshot = loaded
	}

	screen := item.Screen
	img, err := render.Cut(screenshot, item.Rect, item.Figures, func(f *format.Figure) format.ResolvedStyle {
		return project.StyleFor(screen, f)
	}, render.Options{
		OnMissingFont: func(family string) {
			if reportedFonts[family] {
				return
			}
			reportedFonts[family] = true
			onWarn(fmt.Sprintf("this build does not carry the font %q, so text is drawn in %s — the editor falls back the same way, so the image still matches it",
				family, render.FallbackFamily))
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cut %q: %w", item.Cut.Name, err)
	}
	if item.Scale != 1 {
		img = render.Resize(img, item.Width, item.Height)
	}
	return img, nil
}

// performList is what an export would write, without writing any of it.
func performList(dir string) (*export.Plan, error) {
	project, err := openProject(dir)
	if err != nil {
		return nil, err
	}
	return export.Build(project, export.Options{})
}

type validationResult struct {
	OK       bool           `json:"ok"`
	Screens  int            `json:"screens"`
	Cuts     int            `json:"cuts"`
	Problems []lint.Problem `json:"problems"`
}

// performValidate judges a project file. A file that cannot be read at all is
// one problem rather than an error: the caller wants the same shape of answer
// either way, and there is nothing further to check.
func performValidate(dir string) validationResult {
	project, err := format.Load(dir)
	if err != nil {
		problem := lint.Problem{Severity: lint.SeverityError, Message: err.Error()}
		var formatErr *format.Error
		if errors.As(err, &formatErr) {
			problem.Message, problem.Line = formatErr.Message, formatErr.Line
		}
		return validationResult{Problems: []lint.Problem{problem}}
	}

	cuts := 0
	for _, screen := range project.Screens {
		cuts += len(screen.Cuts)
	}

	// The raw bytes again, for line numbers and for the keys nobody read.
	var problems []lint.Problem
	if data, readErr := os.ReadFile(filepath.Join(dir, format.ProjectFile)); readErr == nil {
		problems = lint.Check(dir, data, project)
	}

	result := validationResult{OK: true, Screens: len(project.Screens), Cuts: cuts, Problems: problems}
	for _, problem := range problems {
		if problem.Severity == lint.SeverityError {
			result.OK = false
		}
	}
	return result
}

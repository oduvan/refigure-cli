// Package lint checks a project file for the mistakes that writing one by hand
// — or by program — actually produces.
//
// It is deliberately separate from loading. `format.Load` is lenient on
// purpose: it ignores keys it does not know, so a file written by a newer
// desktop app still exports here. That leniency is exactly what makes a typo
// invisible — `colour:` is simply not `color:`, and nothing complains while the
// figure comes out the default red. Checking belongs here, where being strict
// costs nothing at export time.
//
// Everything reported carries a line, because a caller fixing the file needs to
// find the problem, not just hear about it.
package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/geom"
)

type Severity string

const (
	// SeverityError marks something that makes the export fail, or produce an
	// image that is definitely not what the file describes.
	SeverityError Severity = "error"
	// SeverityWarning marks something that exports, but is almost certainly a
	// mistake — a figure that will not appear anywhere, an image that will
	// overwrite another.
	SeverityWarning Severity = "warning"
)

type Problem struct {
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Line     int      `json:"line,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

// Check returns everything wrong with a project that has already parsed.
// Problems come back in file order, so fixing them top down works.
func Check(dir string, data []byte, project *format.Project) []Problem {
	var problems []Problem

	index := newIndex(data)
	problems = append(problems, index.unknownKeys...)
	problems = append(problems, duplicateIDs(index)...)
	problems = append(problems, references(project, index)...)
	problems = append(problems, geometry(project, index)...)
	problems = append(problems, missingFiles(dir, project, index)...)

	sort.SliceStable(problems, func(a, b int) bool { return problems[a].Line < problems[b].Line })
	return problems
}

/* ------------------------------ the file's shape ----------------------------- */

// shape describes what keys may appear where. A nil shape means "anything",
// which is how values the tool does not interpret are left alone.
type shape struct {
	fields map[string]*shape
	item   *shape
}

func mapping(fields map[string]*shape) *shape { return &shape{fields: fields} }
func sequence(item *shape) *shape             { return &shape{item: item} }

var (
	pointShape = mapping(map[string]*shape{"x": nil, "y": nil})
	rectShape  = mapping(map[string]*shape{"x": nil, "y": nil, "w": nil, "h": nil})
	styleShape = mapping(map[string]*shape{
		"color":  nil,
		"stroke": mapping(map[string]*shape{"width": nil, "style": nil}),
		"font":   mapping(map[string]*shape{"family": nil, "size": nil}),
	})
	figureShape = mapping(map[string]*shape{
		"id": nil, "type": nil, "cut": nil, "style": styleShape,
		"from": pointShape, "to": pointShape, "at": pointShape,
		"rect": rectShape, "text": nil,
	})
	cutShape = mapping(map[string]*shape{
		"id": nil, "name": nil, "rect": rectShape, "style": styleShape,
		"figures": mapping(map[string]*shape{"exclude": nil}),
	})
	screenShape = mapping(map[string]*shape{
		"id": nil, "name": nil, "file": nil, "width": nil, "height": nil,
		"style": styleShape,
		"cuts":  sequence(cutShape),

		// Written by the desktop app and ignored here. Known, so it is not
		// reported as a typo.
		"replacedAt": nil,

		"figures": sequence(figureShape),
	})
	rootShape = mapping(map[string]*shape{
		"version": nil, "name": nil, "style": styleShape,
		"export":  mapping(map[string]*shape{"dest": nil, "format": nil, "quality": nil, "scale": nil}),
		"screens": sequence(screenShape),
	})
)

/* --------------------------------- the index --------------------------------- */

// index is what a second pass over the raw YAML gives that the parsed structs
// cannot: line numbers, and the keys nobody read.
type index struct {
	idLines     map[string][]int
	nameLines   map[string][]int
	unknownKeys []Problem
}

func newIndex(data []byte) *index {
	idx := &index{idLines: map[string][]int{}, nameLines: map[string][]int{}}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return idx
	}
	idx.walk(doc.Content[0], rootShape, "")
	return idx
}

func (idx *index) walk(node *yaml.Node, want *shape, path string) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if want == nil || want.fields == nil {
				continue
			}
			child, known := want.fields[key.Value]
			if !known {
				idx.unknownKeys = append(idx.unknownKeys, unknownKey(key, want, path))
				continue
			}
			switch key.Value {
			case "id":
				idx.idLines[value.Value] = append(idx.idLines[value.Value], value.Line)
			case "name":
				idx.nameLines[value.Value] = append(idx.nameLines[value.Value], value.Line)
			}
			idx.walk(value, child, join(path, key.Value))
		}
	case yaml.SequenceNode:
		if want == nil {
			return
		}
		for _, item := range node.Content {
			idx.walk(item, want.item, path)
		}
	}
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func unknownKey(key *yaml.Node, want *shape, path string) Problem {
	where := "the project"
	if path != "" {
		where = "`" + path + "`"
	}
	problem := Problem{
		Severity: SeverityWarning,
		Message:  fmt.Sprintf("unknown key `%s` in %s — it is ignored", key.Value, where),
		Line:     key.Line,
	}
	if near := closest(key.Value, want.fields); near != "" {
		problem.Hint = fmt.Sprintf("did you mean `%s`?", near)
	}
	return problem
}

// closest finds the known key a typo was probably aiming at.
func closest(word string, fields map[string]*shape) string {
	best, bestDistance := "", 3
	for candidate := range fields {
		if d := distance(word, candidate); d < bestDistance {
			best, bestDistance = candidate, d
		}
	}
	return best
}

func distance(a, b string) int {
	if strings.EqualFold(a, b) {
		return 1
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		copy(previous, current)
	}
	return previous[len(b)]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/* ---------------------------------- checks ----------------------------------- */

func duplicateIDs(idx *index) []Problem {
	var problems []Problem
	for id, lines := range idx.idLines {
		if len(lines) < 2 {
			continue
		}
		sort.Ints(lines)
		for _, line := range lines[1:] {
			problems = append(problems, Problem{
				Severity: SeverityError,
				Message:  fmt.Sprintf("id %q is used more than once", id),
				Line:     line,
				Hint:     fmt.Sprintf("first used on line %d; ids identify one thing", lines[0]),
			})
		}
	}
	return problems
}

// references catches the mistakes that make something silently disappear: a
// figure owned by a cut that does not exist renders in no cut at all.
func references(project *format.Project, idx *index) []Problem {
	var problems []Problem

	for i := range project.Screens {
		screen := &project.Screens[i]

		cuts := map[string]bool{}
		names := map[string]int{}
		for j := range screen.Cuts {
			cut := &screen.Cuts[j]
			cuts[cut.ID] = true
			names[cut.Name]++
		}

		figures := map[string]bool{}
		for j := range screen.Figures {
			figures[screen.Figures[j].ID] = true
		}

		for name, count := range names {
			if count > 1 {
				problems = append(problems, Problem{
					Severity: SeverityError,
					Message:  fmt.Sprintf("two cuts on screen %q are both named %q", screen.Name, name),
					Line:     idx.line(idx.nameLines[name], 1),
					Hint:     "the name becomes the file name, so one image would overwrite the other",
				})
			}
		}

		for j := range screen.Figures {
			figure := &screen.Figures[j]
			if figure.Cut != "" && !cuts[figure.Cut] {
				problems = append(problems, Problem{
					Severity: SeverityError,
					Message: fmt.Sprintf("figure %q belongs to cut %q, which does not exist on screen %q",
						figure.ID, figure.Cut, screen.Name),
					Line: idx.line(idx.idLines[figure.ID], 0),
					Hint: "an owned figure appears only in its own cut, so this one appears nowhere",
				})
			}
			if figure.Type == format.FigureText && strings.TrimSpace(figure.Text) == "" {
				problems = append(problems, Problem{
					Severity: SeverityWarning,
					Message:  fmt.Sprintf("text figure %q has no text", figure.ID),
					Line:     idx.line(idx.idLines[figure.ID], 0),
				})
			}
		}

		for j := range screen.Cuts {
			cut := &screen.Cuts[j]
			if cut.Figures == nil {
				continue
			}
			for _, excluded := range cut.Figures.Exclude {
				if !figures[excluded] {
					problems = append(problems, Problem{
						Severity: SeverityWarning,
						Message: fmt.Sprintf("cut %q excludes figure %q, which is not on screen %q",
							cut.Name, excluded, screen.Name),
						Line: idx.line(idx.idLines[cut.ID], 0),
						Hint: "the exclusion does nothing",
					})
				}
			}
		}
	}
	return problems
}

func geometry(project *format.Project, idx *index) []Problem {
	var problems []Problem

	for i := range project.Screens {
		screen := &project.Screens[i]
		for j := range screen.Cuts {
			cut := &screen.Cuts[j]
			rect := geom.Normalize(cut.Rect)
			line := idx.line(idx.idLines[cut.ID], 0)

			if rect.W < 1 || rect.H < 1 {
				problems = append(problems, Problem{
					Severity: SeverityError,
					Message:  fmt.Sprintf("cut %q is %g×%g, so it has no image to export", cut.Name, rect.W, rect.H),
					Line:     line,
				})
				continue
			}

			// Only checkable when the file records the screenshot's size.
			if screen.Width > 0 && screen.Height > 0 {
				if rect.X >= float64(screen.Width) || rect.Y >= float64(screen.Height) {
					problems = append(problems, Problem{
						Severity: SeverityError,
						Message: fmt.Sprintf("cut %q starts outside screen %q (%d×%d)",
							cut.Name, screen.Name, screen.Width, screen.Height),
						Line: line,
					})
				} else if rect.X+rect.W > float64(screen.Width) || rect.Y+rect.H > float64(screen.Height) {
					problems = append(problems, Problem{
						Severity: SeverityWarning,
						Message: fmt.Sprintf("cut %q runs past the edge of screen %q (%d×%d)",
							cut.Name, screen.Name, screen.Width, screen.Height),
						Line: line,
						Hint: "the part beyond the edge exports as empty space",
					})
				}
			}

			// An owned figure is drawn in its cut's image and nowhere else, so
			// one that falls outside that rectangle is invisible everywhere.
			for k := range screen.Figures {
				figure := &screen.Figures[k]
				if figure.Cut != cut.ID {
					continue
				}
				if !geom.Intersect(geom.Bounds(figure, project.StyleFor(screen, figure)), rect) {
					problems = append(problems, Problem{
						Severity: SeverityWarning,
						Message: fmt.Sprintf("figure %q belongs to cut %q but lies outside it",
							figure.ID, cut.Name),
						Line: idx.line(idx.idLines[figure.ID], 0),
						Hint: "figures are positioned on the screen, not inside the cut",
					})
				}
			}
		}
	}
	return problems
}

func missingFiles(dir string, project *format.Project, idx *index) []Problem {
	var problems []Problem
	for i := range project.Screens {
		screen := &project.Screens[i]
		if screen.File == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, screen.File)); err != nil {
			problems = append(problems, Problem{
				Severity: SeverityError,
				Message:  fmt.Sprintf("screen %q refers to %s, which is not in the project folder", screen.Name, screen.File),
				Line:     idx.line(idx.idLines[screen.ID], 0),
				Hint:     "export needs the file; paths are relative to the project folder",
			})
		}
	}
	return problems
}

func (idx *index) line(lines []int, which int) int {
	if which < len(lines) {
		return lines[which]
	}
	if len(lines) > 0 {
		return lines[len(lines)-1]
	}
	return 0
}

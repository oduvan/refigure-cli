// Package format reads refigure.yaml.
//
// The CLI only ever reads this file. It never writes it, so none of the
// desktop app's careful serialisation — deterministic key order, comments kept
// with their entity by id — needs to exist here.
//
// Unknown keys are ignored on purpose. That is what lets the desktop app add
// fields, or move its own settings into a separate file, without this tool
// caring or needing a matching release.
package format

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ProjectFile is the name of the document inside a project folder.
const ProjectFile = "refigure.yaml"

// FormatVersion is the highest version this tool understands.
const FormatVersion = 1

type Point struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

type Rect struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
	W float64 `yaml:"w"`
	H float64 `yaml:"h"`
}

type Stroke struct {
	Width *float64 `yaml:"width"`
	Style *string  `yaml:"style"`
}

type Font struct {
	Family *string  `yaml:"family"`
	Size   *float64 `yaml:"size"`
}

// Style is partial at every level of the cascade. Resolve turns it into a
// ResolvedStyle before anything is drawn.
type Style struct {
	Color  *string `yaml:"color"`
	Stroke *Stroke `yaml:"stroke"`
	Font   *Font   `yaml:"font"`
}

type FigureType string

const (
	FigureArrow FigureType = "arrow"
	FigureLine  FigureType = "line"
	FigureRect  FigureType = "rect"
	FigureText  FigureType = "text"
)

type Figure struct {
	ID   string     `yaml:"id"`
	Type FigureType `yaml:"type"`
	// Cut is the owning cut's id. A figure that has one belongs to that cut
	// alone; one without belongs to every cut it overlaps.
	Cut   string `yaml:"cut"`
	Rect  *Rect  `yaml:"rect"`
	From  *Point `yaml:"from"`
	To    *Point `yaml:"to"`
	At    *Point `yaml:"at"`
	Text  string `yaml:"text"`
	Style *Style `yaml:"style"`
}

type CutFigures struct {
	Exclude []string `yaml:"exclude"`
}

type Cut struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	Rect Rect   `yaml:"rect"`
	// Figures holds visibility overrides only. The figures themselves live on
	// the screen.
	Figures *CutFigures `yaml:"figures"`
}

type Screen struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	File    string   `yaml:"file"`
	Width   int      `yaml:"width"`
	Height  int      `yaml:"height"`
	Style   *Style   `yaml:"style"`
	Cuts    []Cut    `yaml:"cuts"`
	Figures []Figure `yaml:"figures"`
}

type ExportFormat string

const (
	FormatPNG  ExportFormat = "png"
	FormatJPEG ExportFormat = "jpeg"
	FormatWebP ExportFormat = "webp"
)

type ExportSettings struct {
	Dest    string       `yaml:"dest"`
	Format  ExportFormat `yaml:"format"`
	Quality int          `yaml:"quality"`
	// Scale is either the string "original" or a max width in pixels.
	Scale any `yaml:"scale"`
}

type Project struct {
	Version int            `yaml:"version"`
	Name    string         `yaml:"name"`
	Style   *Style         `yaml:"style"`
	Export  ExportSettings `yaml:"export"`
	Screens []Screen       `yaml:"screens"`

	// Dir is the folder the file was read from. Screen files are relative to it.
	Dir string `yaml:"-"`
}

// Error carries a line number when the YAML parser reported one, so a broken
// file can be pointed at rather than merely rejected.
type Error struct {
	Message string
	Line    int
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("line %d: %s", e.Line, e.Message)
	}
	return e.Message
}

// Load reads and validates the project file in dir.
func Load(dir string) (*Project, error) {
	path := filepath.Join(dir, ProjectFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("no %s found in %s", ProjectFile, dir)}
	}

	var project Project
	if err := yaml.Unmarshal(data, &project); err != nil {
		if typeErr, ok := err.(*yaml.TypeError); ok && len(typeErr.Errors) > 0 {
			return nil, &Error{Message: typeErr.Errors[0]}
		}
		return nil, &Error{Message: err.Error(), Line: lineOf(err)}
	}

	project.Dir = dir
	if err := project.validate(); err != nil {
		return nil, err
	}
	return &project, nil
}

// MaxWidth reports the export downscale cap. ok is false for "original".
func (s ExportSettings) MaxWidth() (int, bool) {
	switch value := s.Scale.(type) {
	case int:
		if value > 0 {
			return value, true
		}
	case float64:
		if value > 0 {
			return int(value), true
		}
	}
	return 0, false
}

func (p *Project) validate() error {
	if p.Version <= 0 {
		return &Error{Message: "missing `version`"}
	}
	if p.Version > FormatVersion {
		return &Error{Message: fmt.Sprintf(
			"this project was written by a newer Refigure (version %d); this tool understands up to %d",
			p.Version, FormatVersion)}
	}
	if p.Export.Format == "" {
		p.Export.Format = FormatPNG
	}
	switch p.Export.Format {
	case FormatPNG, FormatJPEG, FormatWebP:
	default:
		return &Error{Message: fmt.Sprintf("unknown export format %q", p.Export.Format)}
	}
	if p.Export.Quality == 0 {
		p.Export.Quality = 90
	}

	seenScreens := map[string]bool{}
	for i := range p.Screens {
		screen := &p.Screens[i]
		if screen.ID == "" {
			return &Error{Message: fmt.Sprintf("screen %d has no id", i)}
		}
		if seenScreens[screen.ID] {
			return &Error{Message: fmt.Sprintf("duplicate screen id %q", screen.ID)}
		}
		seenScreens[screen.ID] = true
		if screen.File == "" {
			return &Error{Message: fmt.Sprintf("screen %q has no file", screen.label())}
		}
		for j := range screen.Cuts {
			cut := &screen.Cuts[j]
			if cut.ID == "" {
				return &Error{Message: fmt.Sprintf("cut %d on screen %q has no id", j, screen.label())}
			}
			if cut.Name == "" {
				return &Error{Message: fmt.Sprintf("cut %q has no name, so it has no file name", cut.ID)}
			}
		}
		for j := range screen.Figures {
			figure := &screen.Figures[j]
			if figure.ID == "" {
				return &Error{Message: fmt.Sprintf("figure %d on screen %q has no id", j, screen.label())}
			}
			if err := figure.validate(screen.label()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Screen) label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}

func (f *Figure) validate(screen string) error {
	missing := func(field string) error {
		return &Error{Message: fmt.Sprintf("%s figure %q on screen %q has no `%s`", f.Type, f.ID, screen, field)}
	}
	switch f.Type {
	case FigureArrow, FigureLine:
		if f.From == nil {
			return missing("from")
		}
		if f.To == nil {
			return missing("to")
		}
	case FigureRect:
		if f.Rect == nil {
			return missing("rect")
		}
	case FigureText:
		if f.At == nil {
			return missing("at")
		}
	default:
		return &Error{Message: fmt.Sprintf("unknown figure type %q on screen %q", f.Type, screen)}
	}
	return nil
}

// lineOf digs a line number out of a yaml error message such as
// "yaml: line 12: mapping values are not allowed in this context".
func lineOf(err error) int {
	var line int
	if n, scanErr := fmt.Sscanf(err.Error(), "yaml: line %d:", &line); scanErr == nil && n == 1 {
		return line
	}
	return 0
}

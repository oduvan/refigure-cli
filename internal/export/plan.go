// Package export decides what files an export writes, then writes them.
package export

import (
	"fmt"

	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/geom"
)

// Item is one image an export will write.
type Item struct {
	Screen   *format.Screen
	Cut      *format.Cut
	Rect     format.Rect
	Figures  []*format.Figure
	FileName string
	// Width and Height are the size after any downscale.
	Width  int
	Height int
	Scale  float64
}

// Plan is the full list, plus any file names produced more than once.
type Plan struct {
	Items      []Item
	Collisions []string
}

// Extension is the file suffix each format writes.
var Extension = map[format.ExportFormat]string{
	format.FormatPNG:  "png",
	format.FormatJPEG: "jpg",
	format.FormatWebP: "webp",
}

// Options narrow what gets exported.
type Options struct {
	// Only limits the export to these cut or screen names. Empty means all.
	Only []string
	// Format, when set, overrides the project's own setting.
	Format format.ExportFormat
	// MaxWidth caps the output width. Zero means the project's setting applies.
	MaxWidth int
	// Original ignores any downscale, whatever the project says.
	Original bool
}

// Build works out every image an export would produce.
func Build(project *format.Project, opts Options) (*Plan, error) {
	outputFormat := project.Export.Format
	if opts.Format != "" {
		outputFormat = opts.Format
	}
	extension, ok := Extension[outputFormat]
	if !ok {
		return nil, fmt.Errorf("unknown export format %q", outputFormat)
	}

	maxWidth, capped := project.Export.MaxWidth()
	if opts.MaxWidth > 0 {
		maxWidth, capped = opts.MaxWidth, true
	}
	if opts.Original {
		capped = false
	}

	wanted := map[string]bool{}
	for _, name := range opts.Only {
		wanted[name] = true
	}

	plan := &Plan{}
	seen := map[string]int{}

	for i := range project.Screens {
		screen := &project.Screens[i]
		for j := range screen.Cuts {
			cut := &screen.Cuts[j]
			if len(wanted) > 0 && !wanted[cut.Name] && !wanted[screen.Name] {
				continue
			}

			rect := geom.Round(geom.Normalize(cut.Rect))
			if rect.W <= 0 || rect.H <= 0 {
				continue
			}

			scale := 1.0
			if capped && rect.W > float64(maxWidth) {
				// Never upscales, which is why this is only ever a reduction.
				scale = float64(maxWidth) / rect.W
			}

			fileName := cut.Name + "." + extension
			seen[fileName]++

			plan.Items = append(plan.Items, Item{
				Screen:   screen,
				Cut:      cut,
				Rect:     rect,
				Figures:  figuresFor(project, screen, cut),
				FileName: fileName,
				Width:    maxInt(1, int(rect.W*scale+0.5)),
				Height:   maxInt(1, int(rect.H*scale+0.5)),
				Scale:    scale,
			})
		}
	}

	for name, count := range seen {
		if count > 1 {
			plan.Collisions = append(plan.Collisions, name)
		}
	}
	return plan, nil
}

// figuresFor is the figures a cut renders, minus the ones excluded from it.
func figuresFor(project *format.Project, screen *format.Screen, cut *format.Cut) []*format.Figure {
	var figures []*format.Figure
	for i := range screen.Figures {
		figure := &screen.Figures[i]
		if geom.Excluded(cut, figure.ID) {
			continue
		}
		if geom.Includes(cut, figure, project.StyleFor(screen, figure)) {
			figures = append(figures, figure)
		}
	}
	return figures
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

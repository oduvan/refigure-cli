// Package geom holds the rectangle maths that decides which figures a cut
// renders.
//
// These calculations must agree exactly with the desktop app's
// `packages/core/src/geometry/rect.ts`. A figure that overlaps a cut edge here
// but not there would export differently from the preview, which is the one
// thing this tool exists to prevent.
package geom

import "github.com/oduvan/refigure-cli/internal/format"

// Normalize turns a rectangle dragged in any direction into one with positive
// width and height.
func Normalize(r format.Rect) format.Rect {
	x, y, w, h := r.X, r.Y, r.W, r.H
	if w < 0 {
		x, w = x+w, -w
	}
	if h < 0 {
		y, h = y+h, -h
	}
	return format.Rect{X: x, Y: y, W: w, H: h}
}

// Round snaps a rectangle to whole pixels the same way the desktop app does.
func Round(r format.Rect) format.Rect {
	x := round(r.X)
	y := round(r.Y)
	return format.Rect{
		X: x,
		Y: y,
		W: round(r.X+r.W) - x,
		H: round(r.Y+r.H) - y,
	}
}

// Intersect reports whether two rectangles overlap at all.
func Intersect(a, b format.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// textWidthPerChar is the desktop app's rough advance width per point of font
// size. It is deliberately an approximation, and deliberately the *same*
// approximation, so that both tools agree on which cuts a text figure falls in.
// Real glyph metrics would be more accurate and would disagree.
const textWidthPerChar = 0.55

// Bounds is a figure's extent in screen coordinates.
func Bounds(f *format.Figure, style format.ResolvedStyle) format.Rect {
	switch f.Type {
	case format.FigureRect:
		return Normalize(*f.Rect)
	case format.FigureArrow, format.FigureLine:
		return Normalize(format.Rect{
			X: f.From.X,
			Y: f.From.Y,
			W: f.To.X - f.From.X,
			H: f.To.Y - f.From.Y,
		})
	case format.FigureText:
		longest, lines := 0, 1
		current := 0
		for _, r := range f.Text {
			if r == '\n' {
				lines++
				current = 0
				continue
			}
			current++
			if current > longest {
				longest = current
			}
		}
		size := style.FontSize
		return format.Rect{
			X: f.At.X,
			Y: f.At.Y,
			W: max(8, float64(longest)*size*textWidthPerChar),
			H: max(size, float64(lines)*size*1.25),
		}
	}
	return format.Rect{}
}

// Includes reports whether a cut renders a figure.
//
// An owned figure belongs to its cut alone, however the rectangles happen to
// overlap. An unowned figure appears in every cut it overlaps, which is what
// lets one arrow serve two regions.
func Includes(cut *format.Cut, f *format.Figure, style format.ResolvedStyle) bool {
	if f.Cut != "" {
		return f.Cut == cut.ID
	}
	return Intersect(Bounds(f, style), Normalize(cut.Rect))
}

// Excluded reports whether a cut hides a figure it would otherwise render.
func Excluded(cut *format.Cut, figureID string) bool {
	if cut.Figures == nil {
		return false
	}
	for _, id := range cut.Figures.Exclude {
		if id == figureID {
			return true
		}
	}
	return false
}

func round(v float64) float64 {
	if v < 0 {
		return -float64(int(-v + 0.5))
	}
	return float64(int(v + 0.5))
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

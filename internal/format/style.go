package format

// StrokeSolid and StrokeDashed are the two stroke styles.
const (
	StrokeSolid  = "solid"
	StrokeDashed = "dashed"
)

// ResolvedStyle has every slot filled. Nothing is drawn from a partial Style.
type ResolvedStyle struct {
	Color       string
	StrokeWidth float64
	StrokeStyle string
	FontFamily  string
	FontSize    float64
}

// DefaultStyle is the root of the cascade, and must match DEFAULT_STYLE in the
// desktop app's core package.
var DefaultStyle = ResolvedStyle{
	Color:       "#D93A3E",
	StrokeWidth: 3,
	StrokeStyle: StrokeSolid,
	FontFamily:  "Inter",
	FontSize:    15,
}

// How hard a blur or a pixelate hides when the figure does not say, and how
// many box blurs make up one blur. Both must match the desktop app's core
// package — BlurPasses is part of the definition of a blur, not a tuning knob,
// because a region hidden less thoroughly in the file than on the canvas can
// leak what it was asked to hide.
const BlurPasses = 3

// DefaultRedactionStrength is how hard to hide a region the figure says nothing
// about. It comes from the region rather than being a fixed number of pixels: a
// blur of six pixels is a lot on a small icon and nothing at all across a line
// of thirty-pixel text, and a redaction that merely softens its region has
// failed at the only thing it is for.
//
// A pixelate has to be coarser than a blur to hide the same thing. Measured on
// a stroke pattern the shape of writing, a blur of radius 10 leaves about 1% of
// the detail behind and a pixelate of cell 10 leaves 12% — still legible. So
// the two have their own divisors. There is a floor and no ceiling: both
// algorithms run on every pixel of the region exactly once whatever the
// strength, so a bigger number is not slower.
//
// Measured from the figure's own rectangle, before it is clipped to the
// screenshot, which is what the desktop measures too. Must match
// defaultRedactionStrength in the desktop app's core package.
func DefaultRedactionStrength(r Rect, kind FigureType) int {
	shorter := r.W
	if r.H < shorter {
		shorter = r.H
	}
	if shorter < 0 {
		shorter = -shorter
	}
	if kind == FigureBlur {
		return max(6, int(shorter/4+0.5))
	}
	return max(12, int(shorter/2.5+0.5))
}

// Resolve applies the cascade: defaults, then the project style, then the
// screen's override, then the figure's own.
func Resolve(levels ...*Style) ResolvedStyle {
	resolved := DefaultStyle
	for _, level := range levels {
		if level == nil {
			continue
		}
		if level.Color != nil {
			resolved.Color = *level.Color
		}
		if level.Stroke != nil {
			if level.Stroke.Width != nil {
				resolved.StrokeWidth = *level.Stroke.Width
			}
			if level.Stroke.Style != nil {
				resolved.StrokeStyle = *level.Stroke.Style
			}
		}
		if level.Font != nil {
			if level.Font.Family != nil {
				resolved.FontFamily = *level.Font.Family
			}
			if level.Font.Size != nil {
				resolved.FontSize = *level.Font.Size
			}
		}
	}
	return resolved
}

// StyleFor resolves the style a figure is drawn with.
func (p *Project) StyleFor(screen *Screen, figure *Figure) ResolvedStyle {
	var figureStyle *Style
	if figure != nil {
		figureStyle = figure.Style
	}
	return Resolve(p.Style, screen.Style, figureStyle)
}

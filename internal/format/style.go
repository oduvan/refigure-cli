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

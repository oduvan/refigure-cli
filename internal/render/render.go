// Package render draws a cut: the screenshot region, with the figures that
// belong to it on top.
//
// Every constant here is copied from the desktop app's
// `components/editor/FigureShape.tsx`, which draws the canvas, the cut preview
// and — until the desktop switches to this tool — the export as well. If a
// number changes there it must change here, and the conformance fixtures are
// what catch it when it does not.
package render

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/fogleman/gg"
	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/geom"
	xdraw "golang.org/x/image/draw"
)

// Options tune where fonts come from.
type Options struct {
	FontDirs []string
	// OnMissingFont is called once per family that could not be resolved.
	OnMissingFont func(family string)
}

// Cut draws one cut at its natural size.
func Cut(
	screenshot image.Image,
	rect format.Rect,
	figures []*format.Figure,
	styleOf func(*format.Figure) format.ResolvedStyle,
	opts Options,
) (image.Image, error) {
	width, height := int(rect.W), int(rect.H)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("cut has no area")
	}

	ctx := gg.NewContext(width, height)
	// The screenshot is drawn shifted so the cut's top-left sits at the origin;
	// figures then use plain screen coordinates, exactly as they are stored.
	ctx.DrawImage(screenshot, -int(rect.X), -int(rect.Y))
	ctx.Translate(-rect.X, -rect.Y)

	for _, figure := range figures {
		if err := drawFigure(ctx, figure, styleOf(figure), opts); err != nil {
			return nil, err
		}
	}
	return ctx.Image(), nil
}

// Resize scales an image down. It never enlarges — the caller decides the size,
// and export planning never asks for one bigger than the source.
func Resize(src image.Image, width, height int) image.Image {
	bounds := src.Bounds()
	if bounds.Dx() == width && bounds.Dy() == height {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	// CatmullRom is the closest of the standard kernels to the Lanczos3 the
	// desktop app uses through sharp. They are not identical.
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)
	return dst
}

func drawFigure(ctx *gg.Context, f *format.Figure, style format.ResolvedStyle, opts Options) error {
	stroke, err := parseColor(style.Color)
	if err != nil {
		return fmt.Errorf("figure %s: %w", f.ID, err)
	}

	ctx.SetColor(stroke)
	ctx.SetLineWidth(style.StrokeWidth)
	ctx.SetLineCapRound()
	ctx.SetLineJoinRound()
	if style.StrokeStyle == format.StrokeDashed {
		// FigureShape.tsx: [width * 3, width * 2]
		ctx.SetDash(style.StrokeWidth*3, style.StrokeWidth*2)
	} else {
		ctx.SetDash()
	}

	switch f.Type {
	case format.FigureLine:
		ctx.DrawLine(f.From.X, f.From.Y, f.To.X, f.To.Y)
		ctx.Stroke()

	case format.FigureArrow:
		ctx.DrawLine(f.From.X, f.From.Y, f.To.X, f.To.Y)
		ctx.Stroke()
		// Konva draws the head as a filled triangle whose tip is the end point,
		// extending back by pointerLength, pointerWidth across.
		head := math.Max(8, style.StrokeWidth*3)
		angle := math.Atan2(f.To.Y-f.From.Y, f.To.X-f.From.X)
		ctx.SetDash()
		ctx.Push()
		ctx.Translate(f.To.X, f.To.Y)
		ctx.Rotate(angle)
		ctx.MoveTo(0, 0)
		ctx.LineTo(-head, head/2)
		ctx.LineTo(-head, -head/2)
		ctx.ClosePath()
		ctx.Fill()
		ctx.Pop()

	case format.FigureRect:
		r := geom.Normalize(*f.Rect)
		// cornerRadius 2, and no fill — the screenshot must show through.
		ctx.DrawRoundedRectangle(r.X, r.Y, r.W, r.H, 2)
		ctx.Stroke()

	case format.FigureText:
		return drawText(ctx, f, style, stroke, opts)
	}
	return nil
}

func drawText(
	ctx *gg.Context,
	f *format.Figure,
	style format.ResolvedStyle,
	fill color.Color,
	opts Options,
) error {
	face, found := Face(style.FontFamily, style.FontSize, opts.FontDirs)
	if !found && opts.OnMissingFont != nil {
		opts.OnMissingFont(style.FontFamily)
	}
	defer face.Close()

	ctx.SetFontFace(face)
	ctx.SetColor(fill)
	ctx.SetDash()

	// Konva centres each line inside its line box (canvas textBaseline
	// "middle"), so the baseline sits half a line down plus half the em height.
	lineHeight := style.FontSize * 1.25
	metrics := face.Metrics()
	ascent := float64(metrics.Ascent.Round())
	descent := float64(metrics.Descent.Round())

	for i, line := range strings.Split(f.Text, "\n") {
		y := f.At.Y + float64(i)*lineHeight + lineHeight/2 + (ascent-descent)/2
		ctx.DrawString(line, f.At.X, y)
	}
	return nil
}

func parseColor(hex string) (color.Color, error) {
	value := strings.TrimPrefix(hex, "#")
	if len(value) != 6 {
		return nil, fmt.Errorf("colour %q is not a six-digit hex value", hex)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b); err != nil {
		return nil, fmt.Errorf("colour %q is not a six-digit hex value", hex)
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, nil
}

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
	// FigureShape.tsx asks for round caps and joins on arrows and lines, and
	// asks for nothing on a rectangle — so a rectangle gets the canvas defaults,
	// a butt cap and a mitre join. The difference is invisible on a solid
	// rectangle and obvious on a dashed one, where every dash end is a cap.
	// gg has no mitre joiner; the corners here are arcs, so the segments meet
	// almost straight on and bevel is the same to well under a pixel.
	if f.Type == format.FigureRect {
		ctx.SetLineCapButt()
		ctx.SetLineJoinBevel()
	} else {
		ctx.SetLineCapRound()
		ctx.SetLineJoinRound()
	}
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
		// Konva draws the head as a triangle whose tip is the end point,
		// extending back by pointerLength, pointerWidth across — and then both
		// fills and strokes it (Arrow.__fillStroke), never dashed. The outline
		// is what gives the head its size: it widens the triangle by half the
		// stroke on every side, rounds the corners, and covers the round cap
		// the shaft leaves sticking out past the tip. Filling alone draws a
		// sharp sliver with a blob on its point.
		head := math.Max(8, style.StrokeWidth*3)
		angle := math.Atan2(f.To.Y-f.From.Y, f.To.X-f.From.X)
		ctx.SetDash()
		ctx.Push()
		ctx.Translate(f.To.X, f.To.Y)
		ctx.Rotate(angle)
		// The outline starts and ends halfway along the base, not at a corner:
		// gg strokes a closed path as an open polyline, capping both ends
		// instead of joining them, and a cap seam on a corner shows as a spike.
		// Halfway along an edge the two caps fall inside the band and vanish.
		ctx.MoveTo(-head, 0)
		ctx.LineTo(-head, head/2)
		ctx.LineTo(0, 0)
		ctx.LineTo(-head, -head/2)
		ctx.LineTo(-head, 0)
		ctx.FillPreserve()
		ctx.Stroke()
		ctx.Pop()

	case format.FigureRect:
		// cornerRadius 2, and no fill — the screenshot must show through.
		roundedRect(ctx, geom.Normalize(*f.Rect), 2)
		ctx.Stroke()

	case format.FigureText:
		return drawText(ctx, f, style, stroke, opts)
	}
	return nil
}

// roundedRect traces the outline of a rectangle with rounded corners.
//
// gg has DrawRoundedRectangle and it cannot be used: when it strokes, gg drops
// any point within 1/8 px of the one before it (rasterPath, a workaround for
// its own join artefacts), and gg draws each corner as sixteen curve segments.
// At radius 2 those segments are far below the threshold, so whole corners
// disappear from the stroked path and the edges join the wrong points — a
// rectangle comes out visibly skewed, and a small one comes out a blob.
//
// So the corners are traced by hand at about half a pixel per step, coarse
// enough to survive that filter and finer than a pixel of output. The radius
// is clamped to half the shorter side, which is what Konva's
// Util.drawRoundedRectPath does; gg does not clamp at all.
//
// The outline starts part-way along the top edge rather than at a corner: gg
// strokes a closed path as an open polyline and caps both ends, and a cap seam
// on a corner shows, while one inside a straight edge does not.
func roundedRect(ctx *gg.Context, r format.Rect, radius float64) {
	radius = math.Min(radius, math.Min(r.W/2, r.H/2))
	if radius <= 0 {
		ctx.MoveTo(r.X, r.Y)
		ctx.LineTo(r.X+r.W, r.Y)
		ctx.LineTo(r.X+r.W, r.Y+r.H)
		ctx.LineTo(r.X, r.Y+r.H)
		ctx.LineTo(r.X, r.Y)
		return
	}

	steps := int(math.Max(3, math.Ceil(radius*math.Pi/2/0.5)))
	corner := func(cx, cy, from float64) {
		for i := 1; i <= steps; i++ {
			a := from + (math.Pi/2)*float64(i)/float64(steps)
			ctx.LineTo(cx+radius*math.Cos(a), cy+radius*math.Sin(a))
		}
	}

	left, top := r.X, r.Y
	right, bottom := r.X+r.W, r.Y+r.H

	ctx.MoveTo(left+radius, top)
	ctx.LineTo(right-radius, top)
	corner(right-radius, top+radius, -math.Pi/2)
	ctx.LineTo(right, bottom-radius)
	corner(right-radius, bottom-radius, 0)
	ctx.LineTo(left+radius, bottom)
	corner(left+radius, bottom-radius, math.Pi/2)
	ctx.LineTo(left, top+radius)
	corner(left+radius, top+radius, math.Pi)
	ctx.LineTo(left+radius, top)
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

package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/oduvan/refigure-cli/internal/format"
)

func blank(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	return img
}

func red() format.ResolvedStyle {
	style := format.DefaultStyle
	style.Color = "#FF0000"
	return style
}

func styleOf(s format.ResolvedStyle) func(*format.Figure) format.ResolvedStyle {
	return func(*format.Figure) format.ResolvedStyle { return s }
}

// isRed accepts anything clearly red rather than an exact value, because the
// rasteriser antialiases edges.
func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0x8000 && g < 0x8000 && b < 0x8000
}

func TestCutCropsToItsRectangle(t *testing.T) {
	img, err := Cut(blank(400, 300), format.Rect{X: 50, Y: 40, W: 200, H: 100}, nil, styleOf(red()), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 200 || img.Bounds().Dy() != 100 {
		t.Errorf("expected a 200x100 image, got %v", img.Bounds())
	}
}

// Figures are stored in screen coordinates, never cut coordinates. A rectangle
// at screen (60,50) inside a cut starting at (50,40) must land at (10,10).
func TestFiguresAreDrawnInScreenCoordinates(t *testing.T) {
	figure := &format.Figure{
		ID: "f", Type: format.FigureRect,
		Rect: &format.Rect{X: 60, Y: 50, W: 100, H: 60},
	}
	img, err := Cut(blank(400, 300), format.Rect{X: 50, Y: 40, W: 200, H: 100},
		[]*format.Figure{figure}, styleOf(red()), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if !isRed(img.At(10, 10)) {
		t.Error("the rectangle's corner should sit at (10,10) inside the cut")
	}
	if isRed(img.At(150, 90)) {
		t.Error("the rectangle is not filled, so its middle stays white")
	}
}

func TestArrowDrawsAHead(t *testing.T) {
	figure := &format.Figure{
		ID: "f", Type: format.FigureArrow,
		From: &format.Point{X: 10, Y: 50},
		To:   &format.Point{X: 190, Y: 50},
	}
	img, err := Cut(blank(200, 100), format.Rect{X: 0, Y: 0, W: 200, H: 100},
		[]*format.Figure{figure}, styleOf(red()), Options{})
	if err != nil {
		t.Fatal(err)
	}

	if !isRed(img.At(185, 50)) {
		t.Error("the arrow shaft should reach the end point")
	}

	// The head is a filled triangle back from the tip, so a column just behind
	// the end is taller than the 3px shaft in the middle.
	height := func(x int) int {
		n := 0
		for y := 0; y < 100; y++ {
			if isRed(img.At(x, y)) {
				n++
			}
		}
		return n
	}
	if height(184) <= height(100) {
		t.Errorf("head column %d should be taller than shaft column %d", height(184), height(100))
	}
	if isRed(img.At(100, 70)) {
		t.Error("nothing should be drawn well off the line")
	}
}

func TestDashedStrokeLeavesGaps(t *testing.T) {
	style := red()
	style.StrokeStyle = format.StrokeDashed
	style.StrokeWidth = 4

	figure := &format.Figure{
		ID: "f", Type: format.FigureLine,
		From: &format.Point{X: 0, Y: 50},
		To:   &format.Point{X: 200, Y: 50},
	}
	img, err := Cut(blank(200, 100), format.Rect{X: 0, Y: 0, W: 200, H: 100},
		[]*format.Figure{figure}, styleOf(style), Options{})
	if err != nil {
		t.Fatal(err)
	}

	painted := 0
	for x := 0; x < 200; x++ {
		if isRed(img.At(x, 50)) {
			painted++
		}
	}
	if painted == 0 || painted > 170 {
		t.Errorf("a dashed line should paint some of the row but not all of it, got %d of 200", painted)
	}
}

func TestResizeShrinks(t *testing.T) {
	out := Resize(blank(800, 600), 400, 300)
	if out.Bounds().Dx() != 400 || out.Bounds().Dy() != 300 {
		t.Errorf("got %v", out.Bounds())
	}
}

func TestTextFallsBackWhenTheFontIsMissing(t *testing.T) {
	var missing string
	figure := &format.Figure{
		ID: "f", Type: format.FigureText,
		At: &format.Point{X: 10, Y: 10}, Text: "hello",
	}
	style := red()
	style.FontFamily = "AFontThatDoesNotExist"

	_, err := Cut(blank(200, 100), format.Rect{X: 0, Y: 0, W: 200, H: 100},
		[]*format.Figure{figure}, styleOf(style),
		Options{OnMissingFont: func(family string) { missing = family }})
	if err != nil {
		t.Fatal(err)
	}
	if missing != "AFontThatDoesNotExist" {
		t.Error("a missing family must be reported, because the output will not match the editor")
	}
}

func TestBadColourIsRejected(t *testing.T) {
	style := red()
	style.Color = "not-a-colour"
	figure := &format.Figure{ID: "f", Type: format.FigureRect, Rect: &format.Rect{X: 0, Y: 0, W: 10, H: 10}}

	if _, err := Cut(blank(50, 50), format.Rect{X: 0, Y: 0, W: 50, H: 50},
		[]*format.Figure{figure}, styleOf(style), Options{}); err == nil {
		t.Error("expected an error")
	}
}

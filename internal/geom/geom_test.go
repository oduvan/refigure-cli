package geom

import (
	"testing"

	"github.com/oduvan/refigure-cli/internal/format"
)

// These expectations are lifted from the desktop app's geometry tests. They
// exist so the two tools agree on which cuts a figure falls into; if this file
// starts failing, the exports have drifted from the previews.
func TestNormalizeHandlesAnyDragDirection(t *testing.T) {
	got := Normalize(format.Rect{X: 100, Y: 100, W: -40, H: -20})
	want := format.Rect{X: 60, Y: 80, W: 40, H: 20}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestArrowBoundsCoverRightToLeft(t *testing.T) {
	arrow := &format.Figure{
		Type: format.FigureArrow,
		From: &format.Point{X: 400, Y: 300},
		To:   &format.Point{X: 200, Y: 100},
	}
	got := Bounds(arrow, format.DefaultStyle)
	want := format.Rect{X: 200, Y: 100, W: 200, H: 200}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestMembershipByOverlap(t *testing.T) {
	arrow := &format.Figure{
		Type: format.FigureArrow,
		From: &format.Point{X: 400, Y: 300},
		To:   &format.Point{X: 200, Y: 100},
	}
	near := &format.Cut{ID: "a", Rect: format.Rect{X: 0, Y: 0, W: 250, H: 250}}
	far := &format.Cut{ID: "b", Rect: format.Rect{X: 500, Y: 500, W: 100, H: 100}}

	if !Includes(near, arrow, format.DefaultStyle) {
		t.Error("an overlapping cut should include an unowned figure")
	}
	if Includes(far, arrow, format.DefaultStyle) {
		t.Error("a distant cut should not")
	}
}

func TestOwnershipOverridesOverlap(t *testing.T) {
	owned := &format.Figure{
		Type: format.FigureRect, Cut: "a",
		Rect: &format.Rect{X: 10, Y: 10, W: 10, H: 10},
	}
	owner := &format.Cut{ID: "a", Rect: format.Rect{X: 0, Y: 0, W: 500, H: 500}}
	other := &format.Cut{ID: "b", Rect: format.Rect{X: 0, Y: 0, W: 500, H: 500}}

	if !Includes(owner, owned, format.DefaultStyle) {
		t.Error("the owner always renders it")
	}
	if Includes(other, owned, format.DefaultStyle) {
		t.Error("no other cut may, however much they overlap")
	}
}

// The width guess is deliberately crude, and deliberately identical to the
// desktop's. Real glyph metrics would be more accurate and would disagree.
func TestTextBoundsUseTheSharedApproximation(t *testing.T) {
	text := &format.Figure{Type: format.FigureText, At: &format.Point{X: 0, Y: 0}, Text: "hello"}
	got := Bounds(text, format.ResolvedStyle{FontSize: 20})

	// Compared loosely: Go evaluates constant arithmetic exactly, while both
	// this tool and the desktop app multiply float64s at runtime.
	if !close(got.W, 5*20*0.55) {
		t.Errorf("width should be chars x size x 0.55, got %v", got.W)
	}
	if got.H != 25 {
		t.Errorf("one line at size 20 should be 25 high, got %v", got.H)
	}
}

func TestMultilineTextUsesTheLongestLine(t *testing.T) {
	text := &format.Figure{Type: format.FigureText, At: &format.Point{X: 0, Y: 0}, Text: "ab\nabcd\na"}
	got := Bounds(text, format.ResolvedStyle{FontSize: 10})

	if !close(got.W, 4*10*0.55) {
		t.Errorf("width comes from the longest line, got %v", got.W)
	}
	if got.H != 3*10*1.25 {
		t.Errorf("height is lines x size x 1.25, got %v", got.H)
	}
}

func TestExclusion(t *testing.T) {
	cut := &format.Cut{ID: "a", Figures: &format.CutFigures{Exclude: []string{"fig_1"}}}
	if !Excluded(cut, "fig_1") {
		t.Error("an excluded id should report as excluded")
	}
	if Excluded(cut, "fig_2") {
		t.Error("other figures are not excluded")
	}
	if Excluded(&format.Cut{ID: "b"}, "fig_1") {
		t.Error("a cut with no overrides excludes nothing")
	}
}

func close(a, b float64) bool {
	diff := a - b
	return diff < 1e-9 && diff > -1e-9
}

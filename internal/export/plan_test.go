package export

import (
	"testing"

	"github.com/oduvan/refigure-cli/internal/format"
)

func ptr[T any](v T) *T { return &v }

// project builds a screen with two overlapping cuts and three figures: one
// owned by the first cut, one unowned, and one far away.
func project() *format.Project {
	return &format.Project{
		Version: 1,
		Export:  format.ExportSettings{Format: format.FormatPNG, Quality: 90, Scale: "original"},
		Screens: []format.Screen{{
			ID: "scr_1", Name: "connect", File: "connect.png", Width: 1200, Height: 800,
			Cuts: []format.Cut{
				{ID: "cut_a", Name: "token", Rect: format.Rect{X: 0, Y: 0, W: 800, H: 600}},
				{ID: "cut_b", Name: "full", Rect: format.Rect{X: 0, Y: 0, W: 1200, H: 700}},
			},
			Figures: []format.Figure{
				{ID: "fig_owned", Type: format.FigureRect, Cut: "cut_a",
					Rect: &format.Rect{X: 10, Y: 10, W: 100, H: 100}},
				{ID: "fig_free", Type: format.FigureArrow,
					From: &format.Point{X: 20, Y: 20}, To: &format.Point{X: 200, Y: 200}},
				{ID: "fig_away", Type: format.FigureRect,
					Rect: &format.Rect{X: 1150, Y: 750, W: 40, H: 40}},
			},
		}},
	}
}

func itemFor(t *testing.T, plan *Plan, fileName string) Item {
	t.Helper()
	for _, item := range plan.Items {
		if item.FileName == fileName {
			return item
		}
	}
	t.Fatalf("no item named %s", fileName)
	return Item{}
}

func figureIDs(item Item) []string {
	ids := make([]string, 0, len(item.Figures))
	for _, f := range item.Figures {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestOwnedFigureBelongsToItsCutAlone(t *testing.T) {
	plan, err := Build(project(), Options{})
	if err != nil {
		t.Fatal(err)
	}

	token := figureIDs(itemFor(t, plan, "token.png"))
	full := figureIDs(itemFor(t, plan, "full.png"))

	// Both cuts cover fig_owned, but only its owner renders it.
	if len(token) != 2 || token[0] != "fig_owned" || token[1] != "fig_free" {
		t.Errorf("token cut should render the owned figure and the free one, got %v", token)
	}
	// The unowned figure appears in every cut it overlaps; the distant one in none.
	if len(full) != 1 || full[0] != "fig_free" {
		t.Errorf("full cut should render only the unowned figure, got %v", full)
	}
}

func TestExclusionBeatsMembership(t *testing.T) {
	p := project()
	p.Screens[0].Cuts[0].Figures = &format.CutFigures{Exclude: []string{"fig_owned"}}

	plan, err := Build(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	got := figureIDs(itemFor(t, plan, "token.png"))
	if len(got) != 1 || got[0] != "fig_free" {
		t.Errorf("an excluded figure must not render even in its owner, got %v", got)
	}
}

func TestDownscaleNeverEnlarges(t *testing.T) {
	p := project()
	p.Export.Scale = 400

	plan, err := Build(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	token := itemFor(t, plan, "token.png")
	if token.Width != 400 || token.Height != 300 {
		t.Errorf("800x600 capped at 400 should be 400x300, got %dx%d", token.Width, token.Height)
	}

	// A cut already narrower than the cap is left alone.
	p.Export.Scale = 5000
	plan, _ = Build(p, Options{})
	token = itemFor(t, plan, "token.png")
	if token.Width != 800 || token.Scale != 1 {
		t.Errorf("a cap wider than the cut must change nothing, got width %d scale %v", token.Width, token.Scale)
	}
}

func TestFormatDecidesTheExtension(t *testing.T) {
	plan, err := Build(project(), Options{Format: format.FormatJPEG})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := Extension[format.FormatJPEG]; !ok {
		t.Fatal("jpeg has no extension")
	}
	itemFor(t, plan, "token.jpg")
}

func TestCollidingNamesAreReported(t *testing.T) {
	p := project()
	p.Screens[0].Cuts[1].Name = "token"

	plan, err := Build(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Collisions) != 1 || plan.Collisions[0] != "token.png" {
		t.Errorf("two cuts named token should collide, got %v", plan.Collisions)
	}
}

func TestOnlyNarrowsBySelection(t *testing.T) {
	plan, err := Build(project(), Options{Only: []string{"full"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].FileName != "full.png" {
		t.Errorf("--only full should leave one item, got %d", len(plan.Items))
	}

	// A screen name selects all of its cuts.
	plan, _ = Build(project(), Options{Only: []string{"connect"}})
	if len(plan.Items) != 2 {
		t.Errorf("--only connect should select the whole screen, got %d", len(plan.Items))
	}
}

func TestZeroSizedCutsAreSkipped(t *testing.T) {
	p := project()
	p.Screens[0].Cuts[0].Rect = format.Rect{X: 10, Y: 10, W: 0, H: 0}

	plan, err := Build(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 {
		t.Errorf("a cut with no area writes no file, got %d items", len(plan.Items))
	}
}

func TestStyleCascade(t *testing.T) {
	p := project()
	p.Style = &format.Style{Color: ptr("#111111")}
	p.Screens[0].Style = &format.Style{Stroke: &format.Stroke{Width: ptr(8.0)}}
	p.Screens[0].Figures[0].Style = &format.Style{Color: ptr("#222222")}

	screen := &p.Screens[0]
	owned := p.StyleFor(screen, &screen.Figures[0])
	free := p.StyleFor(screen, &screen.Figures[1])

	if owned.Color != "#222222" {
		t.Errorf("the figure's own colour wins, got %s", owned.Color)
	}
	if free.Color != "#111111" {
		t.Errorf("a figure with no colour takes the project's, got %s", free.Color)
	}
	if owned.StrokeWidth != 8 {
		t.Errorf("the screen's stroke width applies to both, got %v", owned.StrokeWidth)
	}
	if owned.FontFamily != format.DefaultStyle.FontFamily {
		t.Errorf("an unset slot falls back to the default, got %s", owned.FontFamily)
	}
}

package render

import "testing"

// The binary carries its fonts so that a build machine with none installed
// draws what the author saw. If one of them stops parsing, every text figure
// silently becomes the fallback.
func TestEveryBundledFamilyLoads(t *testing.T) {
	for _, family := range Families() {
		face, found := Face(family, 15)
		if !found {
			t.Errorf("%q is listed as bundled but was not found", family)
			continue
		}
		if _, ok := face.GlyphAdvance('M'); !ok {
			t.Errorf("%q has no glyph for 'M'", family)
		}
		face.Close()
	}
}

// A family we do not carry is drawn in the fallback — and the desktop app does
// the same, so the exported image still matches the preview. The caller is told
// so it can say the project did not get the typeface it named.
func TestAnUnknownFamilyFallsBack(t *testing.T) {
	face, found := Face("Georgia", 15)
	defer face.Close()
	if found {
		t.Error("Georgia is not bundled, so it must not report as found")
	}

	fallback, _ := Face(FallbackFamily, 15)
	defer fallback.Close()
	want, _ := fallback.GlyphAdvance('M')
	got, _ := face.GlyphAdvance('M')
	if got != want {
		t.Errorf("fallback drew a different face: 'M' advances %v and %v", got, want)
	}
}

// The desktop's picker offers exactly these, and DEFAULT_STYLE names the first.
func TestTheBundledListIsWhatTheDesktopShips(t *testing.T) {
	want := []string{"Inter", "JetBrains Mono", "Source Serif 4"}
	got := Families()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if FallbackFamily != "Inter" {
		t.Errorf("the fallback is %q; DEFAULT_STYLE names Inter", FallbackFamily)
	}
}

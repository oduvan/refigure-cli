package render

import (
	"fmt"
	"testing"

	"github.com/oduvan/refigure-cli/internal/format"
)

// The same pseudo-random image the desktop's checker builds, so both sides
// hash the same bytes.
func sample(w, h int) []byte {
	data := make([]byte, w*h*4)
	// MINSTD: x*16807 stays under 2^53, so JS and Go walk the same sequence.
	x := int64(42)
	for i := range data {
		x = (x * 16807) % 2147483647
		data[i] = byte(x % 256)
	}
	return data
}

func fnv1a(data []byte) string {
	h := uint32(0x811c9dc5)
	for _, b := range data {
		h ^= uint32(b)
		h *= 0x01000193
	}
	return fmt.Sprintf("%08x", h)
}

// Hiding a region is the one figure where "close enough" can leak what it was
// asked to hide, so the two implementations are held to the byte. These digests
// come from running packages/core/src/render/redact.ts over the same input; if
// one side is edited without the other, this fails.
func TestRedactionMatchesTheDesktopByteForByte(t *testing.T) {
	cases := []struct {
		name   string
		w, h   int
		run    func([]byte, int, int)
		digest string
	}{
		{"pixelate 37x23 cell=3", 37, 23, func(d []byte, w, h int) { pixelate(d, w, h, 3) }, "3b9c45be"},
		{"pixelate 37x23 cell=12", 37, 23, func(d []byte, w, h int) { pixelate(d, w, h, 12) }, "5ea2fcac"},
		{"blur 37x23 radius=1", 37, 23, func(d []byte, w, h int) { blur(d, w, h, 1, 3) }, "cee9780d"},
		{"blur 37x23 radius=6", 37, 23, func(d []byte, w, h int) { blur(d, w, h, 6, 3) }, "a9097a60"},
		{"pixelate 64x64 cell=3", 64, 64, func(d []byte, w, h int) { pixelate(d, w, h, 3) }, "4ef649a2"},
		{"pixelate 64x64 cell=12", 64, 64, func(d []byte, w, h int) { pixelate(d, w, h, 12) }, "7c62ebc5"},
		{"blur 64x64 radius=1", 64, 64, func(d []byte, w, h int) { blur(d, w, h, 1, 3) }, "70a87cfd"},
		{"blur 64x64 radius=6", 64, 64, func(d []byte, w, h int) { blur(d, w, h, 6, 3) }, "7a846855"},
		{"pixelate 5x200 cell=3", 5, 200, func(d []byte, w, h int) { pixelate(d, w, h, 3) }, "ea89120a"},
		{"pixelate 5x200 cell=12", 5, 200, func(d []byte, w, h int) { pixelate(d, w, h, 12) }, "90d484f5"},
		{"blur 5x200 radius=1", 5, 200, func(d []byte, w, h int) { blur(d, w, h, 1, 3) }, "2de2b597"},
		{"blur 5x200 radius=6", 5, 200, func(d []byte, w, h int) { blur(d, w, h, 6, 3) }, "6ceee04a"},
	}

	for _, c := range cases {
		data := sample(c.w, c.h)
		c.run(data, c.w, c.h)
		if got := fnv1a(data); got != c.digest {
			t.Errorf("%s: got %s, the desktop gets %s", c.name, got, c.digest)
		}
	}
}

// A region smaller than one block, and a region of nothing, are both drawable.
func TestRedactionHandlesTinyRegions(t *testing.T) {
	one := []byte{10, 20, 30, 255}
	pixelate(one, 1, 1, 12)
	if one[0] != 10 || one[3] != 255 {
		t.Errorf("a single pixel averages to itself, got %v", one)
	}
	blur(one, 1, 1, 6, 3)
	if one[0] != 10 {
		t.Errorf("a single pixel blurs to itself, got %v", one)
	}
	pixelate(nil, 0, 0, 12)
	blur(nil, 0, 0, 6, 3)
}

// The generator itself has to walk the same sequence, or the digests above
// would compare different pictures and agree by accident.
func TestTheSampleGeneratorMatchesTheDesktop(t *testing.T) {
	got := sample(4, 1)[:8]
	want := []byte{102, 143, 185, 247, 106, 217, 113, 200}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sample bytes %v, the desktop produces %v", got, want)
		}
	}
}

// The default strength is a copied rule, like the drawing constants: the
// desktop works the same number out of the same rectangle, or a region would be
// hidden by a different amount in the file than on the canvas. These values
// come from `defaultRedactionStrength` in the desktop's core package.
func TestDefaultStrengthMatchesTheDesktop(t *testing.T) {
	cases := []struct {
		w, h float64
		kind format.FigureType
		want int
	}{
		{400, 40, format.FigureBlur, 10},
		{400, 40, format.FigurePixelate, 16},
		{560, 120, format.FigureBlur, 30},
		{560, 120, format.FigurePixelate, 48},
		// A floor, because a sliver is still worth hiding. No ceiling: it would
		// only weaken a large region, and costs nothing to leave off — both
		// algorithms touch every pixel once whatever the strength.
		{500, 8, format.FigureBlur, 6},
		{500, 8, format.FigurePixelate, 12},
		{800, 600, format.FigureBlur, 150},
	}
	for _, c := range cases {
		got := format.DefaultRedactionStrength(format.Rect{W: c.w, H: c.h}, c.kind)
		if got != c.want {
			t.Errorf("%s over %gx%g: got %d, the desktop gets %d", c.kind, c.w, c.h, got, c.want)
		}
	}
}

package render

import (
	_ "embed"
	"sort"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

// Text used to be the weakest point of matching the desktop app, and this is
// where that was fixed.
//
// The desktop resolved a family through the browser and this tool resolved it
// through the operating system's font folders, so the same project drew one
// typeface in the editor and another in the file: neither machine had Inter, so
// Chromium fell back to a serif and this tool to a bundled sans. Menlo was
// worse — macOS ships it as a `.ttc` collection this parser cannot read, so the
// editor drew it properly and the export never could.
//
// Now neither side looks at the machine. Both carry the same three families and
// draw from the same files, so the glyphs cannot disagree, and a build machine
// with no fonts installed exports exactly what the author saw. The desktop
// ships these same `.ttf`s and its picker offers these families and no others.
// Adding a family means adding it in both repositories, in one change.
//
// What is still not identical is the baseline — see drawText and CLAUDE.md.

//go:embed fonts/Inter-SemiBold.ttf
var interSemiBold []byte

//go:embed fonts/SourceSerif4-Semibold.ttf
var sourceSerifSemiBold []byte

//go:embed fonts/JetBrainsMono-SemiBold.ttf
var jetBrainsMonoSemiBold []byte

// Every figure is drawn at weight 600 — FigureShape.tsx hard-codes it — so one
// cut per family is all either side needs.
var bundled = map[string][]byte{
	"Inter":          interSemiBold,
	"Source Serif 4": sourceSerifSemiBold,
	"JetBrains Mono": jetBrainsMonoSemiBold,
}

// FallbackFamily is what a family we do not carry is drawn in, here and in the
// desktop app alike, so a project naming a font from before these three were
// fixed still exports exactly what its preview shows.
const FallbackFamily = "Inter"

// Families lists what can be drawn.
func Families() []string {
	names := make([]string, 0, len(bundled))
	for name := range bundled {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var (
	fontCache   = map[string]*truetype.Font{}
	fontCacheMu sync.Mutex
)

// Face returns a face for the family at the given size, and reports whether the
// family is one we carry. A false means the fallback was drawn: the image still
// matches the editor exactly, but it is not the typeface the project asked for.
func Face(family string, size float64) (font.Face, bool) {
	parsed, found := lookup(family)
	return truetype.NewFace(parsed, &truetype.Options{
		Size: size,
		DPI:  72,
		// No hinting, measured rather than assumed: Chromium on macOS draws
		// through CoreText, which does not apply TrueType hinting, so hinting
		// here moved stems onto different pixels from the ones the editor drew.
		// Unhinted, Inter at 15 px lands on exactly the same rows.
		Hinting: font.HintingNone,
	}), found
}

func lookup(family string) (*truetype.Font, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()

	data, found := bundled[family]
	key := family
	if !found {
		key, data = FallbackFamily, bundled[FallbackFamily]
	}
	if cached, ok := fontCache[key]; ok {
		return cached, found
	}

	parsed, err := truetype.Parse(data)
	if err != nil {
		// An embedded file that will not parse is a broken build, not a broken
		// project: these bytes ship inside the binary and nothing can change them.
		panic("bundled font " + key + " failed to parse: " + err.Error())
	}
	fontCache[key] = parsed
	return parsed, found
}

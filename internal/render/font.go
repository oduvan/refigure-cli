package render

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
)

// Text is the weakest point of matching the desktop app, and this is where it
// lives.
//
// The desktop draws text through the browser's engine, which resolves families
// through the operating system and hints glyphs its own way. This tool resolves
// a font file itself. Where the file cannot be found — most macOS system fonts
// ship as .ttc collections, which this parser cannot read — it falls back to a
// bundled face, and the text will not match the preview.
//
// The fix is to ship the project's fonts with the project. Until then, the
// conformance fixtures are what bound the difference.

var (
	fontCache   = map[string]*truetype.Font{}
	fontCacheMu sync.Mutex
	fallback    *truetype.Font
	fallbackOne sync.Once
)

// FontDirs are searched in order, before the bundled fallback.
func FontDirs() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return []string{
			filepath.Join(home, "Library/Fonts"),
			"/Library/Fonts",
			"/System/Library/Fonts",
			"/System/Library/Fonts/Supplemental",
		}
	case "windows":
		return []string{`C:\Windows\Fonts`}
	default:
		return []string{
			filepath.Join(home, ".local/share/fonts"),
			filepath.Join(home, ".fonts"),
			"/usr/share/fonts",
			"/usr/local/share/fonts",
		}
	}
}

// Face returns a face for the family at the given size, and reports whether the
// requested family was actually found. A false means the fallback was used and
// the output will not match the desktop preview.
func Face(family string, size float64, extraDirs []string) (font.Face, bool) {
	parsed, found := lookup(family, extraDirs)
	return truetype.NewFace(parsed, &truetype.Options{
		Size: size,
		DPI:  72,
		// Full hinting is closest to what a browser does at small sizes.
		Hinting: font.HintingFull,
	}), found
}

func lookup(family string, extraDirs []string) (*truetype.Font, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if cached, ok := fontCache[family]; ok {
		return cached, cached != loadFallback()
	}

	// The desktop draws text at weight 600, so a semibold cut is preferred.
	suffixes := []string{"-SemiBold", "-Semibold", "-semibold", "-Medium", "-Bold", "-Regular", ""}
	dirs := append(append([]string{}, extraDirs...), FontDirs()...)

	for _, dir := range dirs {
		for _, suffix := range suffixes {
			for _, ext := range []string{".ttf", ".otf"} {
				path := filepath.Join(dir, family+suffix+ext)
				if parsed, err := parse(path); err == nil {
					fontCache[family] = parsed
					return parsed, true
				}
			}
		}
	}

	parsed := loadFallback()
	fontCache[family] = parsed
	return parsed, false
}

func parse(path string) (*truetype.Font, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return truetype.Parse(data)
}

func loadFallback() *truetype.Font {
	fallbackOne.Do(func() {
		parsed, err := truetype.Parse(gobold.TTF)
		if err != nil {
			panic("bundled fallback font failed to parse: " + err.Error())
		}
		fallback = parsed
	})
	return fallback
}

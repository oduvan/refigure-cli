package export

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	"github.com/gen2brain/webp"

	"github.com/oduvan/refigure-cli/internal/format"
)

// Encode writes one image in the requested format.
func Encode(img image.Image, path string, outputFormat format.ExportFormat, quality int) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	switch outputFormat {
	case format.FormatPNG:
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		return encoder.Encode(file, img)
	case format.FormatJPEG:
		return jpeg.Encode(file, flatten(img), &jpeg.Options{Quality: quality})
	case format.FormatWebP:
		// libwebp compiled to WASM and run by wazero: real lossy WebP with a
		// working quality knob, and still no cgo. It costs about 3.5 MB of
		// binary, which is the right trade for matching what the desktop app's
		// sharp pipeline produces.
		return webp.Encode(file, img, webp.Options{Quality: quality})
	}
	return fmt.Errorf("unknown export format %q", outputFormat)
}

// EnsureDir creates the destination folder if it is missing.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// Existing reports which of these names are already present in dir.
func Existing(dir string, names []string) []string {
	var found []string
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// flatten puts a transparent image on white, because JPEG has no alpha.
func flatten(src image.Image) image.Image {
	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := src.At(x, y).RGBA()
			if a == 0xffff {
				dst.Set(x, y, src.At(x, y))
				continue
			}
			alpha := float64(a) / 0xffff
			blend := func(c uint32) uint8 {
				return uint8((float64(c)/0xffff*alpha + (1 - alpha)) * 255)
			}
			dst.Pix[dst.PixOffset(x, y)+0] = blend(r)
			dst.Pix[dst.PixOffset(x, y)+1] = blend(g)
			dst.Pix[dst.PixOffset(x, y)+2] = blend(b)
			dst.Pix[dst.PixOffset(x, y)+3] = 255
		}
	}
	return dst
}

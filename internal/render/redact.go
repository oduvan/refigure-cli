package render

// Hiding a region: the pixels behind a blur or a pixelate figure.
//
// These two functions mirror the desktop's `packages/core/src/render/redact.ts`
// line for line, and that file is the definition. A redaction that came out
// softer here than on the canvas would be worse than a wrong colour — it is the
// one kind of figure where being approximately right can leak what it was asked
// to hide. So every detail is part of the contract: the pass count, the order
// of the sweeps, edge clamping, and integer division that truncates.
//
// Both take a tightly packed RGBA buffer, width*height*4 bytes, and edit it in
// place, so the two implementations can be compared byte for byte.

// pixelate replaces each cell-sized block with its own average.
//
// Blocks start at the region's top-left, so what a pixelate hides does not
// shift when the figure is dragged: the grid belongs to the figure, not to the
// screenshot. A block at the right or bottom edge is clipped and averages only
// the pixels it really covers.
func pixelate(data []byte, width, height, cell int) {
	if width <= 0 || height <= 0 {
		return
	}
	size := cell
	if size < 1 {
		size = 1
	}

	for blockY := 0; blockY < height; blockY += size {
		yEnd := blockY + size
		if yEnd > height {
			yEnd = height
		}
		for blockX := 0; blockX < width; blockX += size {
			xEnd := blockX + size
			if xEnd > width {
				xEnd = width
			}

			var r, g, b, a int
			for y := blockY; y < yEnd; y++ {
				for x := blockX; x < xEnd; x++ {
					i := (y*width + x) * 4
					r += int(data[i])
					g += int(data[i+1])
					b += int(data[i+2])
					a += int(data[i+3])
				}
			}

			count := (xEnd - blockX) * (yEnd - blockY)
			r /= count
			g /= count
			b /= count
			a /= count

			for y := blockY; y < yEnd; y++ {
				for x := blockX; x < xEnd; x++ {
					i := (y*width + x) * 4
					data[i] = byte(r)
					data[i+1] = byte(g)
					data[i+2] = byte(b)
					data[i+3] = byte(a)
				}
			}
		}
	}
}

// blur smears the region with `passes` box blurs of the given radius.
//
// Three box blurs approximate a Gaussian closely enough that nobody can read
// what was under them, and a box blur is exactly reproducible in another
// language, which a real Gaussian kernel of floating-point weights is not.
//
// Each pass sweeps horizontally and then vertically. A window that runs off the
// edge repeats the edge pixel rather than shrinking, so the window is always
// 2*radius+1 wide and every output pixel divides by the same count — shrinking
// windows would leave a visibly sharper rim around the region.
func blur(data []byte, width, height, radius, passes int) {
	if width <= 0 || height <= 0 {
		return
	}
	r := radius
	if r < 1 {
		r = 1
	}
	scratch := make([]byte, len(data))

	for pass := 0; pass < passes; pass++ {
		boxBlurPass(data, scratch, width, height, r, true)
		boxBlurPass(scratch, data, width, height, r, false)
	}
}

// boxBlurPass is one sweep. horizontal false runs the same window down each
// column.
func boxBlurPass(from, to []byte, width, height, radius int, horizontal bool) {
	lineCount, lineLength := width, height
	step, lineStep := width*4, 4
	if horizontal {
		lineCount, lineLength = height, width
		step, lineStep = 4, width*4
	}
	window := radius*2 + 1
	last := lineLength - 1

	for line := 0; line < lineCount; line++ {
		base := line * lineStep

		for channel := 0; channel < 4; channel++ {
			at := func(index int) int {
				return int(from[base+index*step+channel])
			}

			// The window starts centred on pixel 0, so it holds the edge pixel
			// radius+1 times over.
			sum := at(0) * (radius + 1)
			for i := 1; i <= radius; i++ {
				sum += at(min(i, last))
			}

			for i := 0; i <= last; i++ {
				to[base+i*step+channel] = byte(sum / window)
				sum += at(min(i+radius+1, last)) - at(max(i-radius, 0))
			}
		}
	}
}

# refigure

Export a [Refigure](https://github.com/oduvan/refigure) project to images, from
the command line.

A Refigure project is a folder: one `refigure.yaml` and the screenshots it
references. The annotations live in the YAML as data, never baked into pixels.
This tool reads that folder and writes one image per cut — the same job the
desktop app's Export dialog does, with no window and no desktop app installed.

```bash
refigure export ./docs/screenshots --out ./site/static/img
```

That makes it a CI step. Commit the project folder, regenerate the images on
every push, and the tutorial images in your docs are never stale again.

Single static binary, no runtime, no dependencies to install.

## Install

Download a binary for your platform from
[Releases](https://github.com/oduvan/refigure-cli/releases), unpack it, and put
`refigure` somewhere on your `PATH`.

```bash
# macOS (Apple silicon) — adjust the version and platform
curl -sSL https://github.com/oduvan/refigure-cli/releases/latest/download/refigure_v0.1.0_darwin_arm64.tar.gz | tar -xz
sudo mv refigure /usr/local/bin/
```

macOS may refuse the binary the first time because it is not notarised: open
System Settings → Privacy & Security and allow it, or run
`xattr -d com.apple.quarantine refigure`.

With a Go toolchain:

```bash
go install github.com/oduvan/refigure-cli/cmd/refigure@latest
```

## Use

```
refigure export [project] [flags]   write one image per cut
refigure list   [project]           show the cuts and the file names they produce
refigure validate [project]         check refigure.yaml, print nothing if it is fine
refigure version
```

The project defaults to the current directory.

| Export flag | What it does |
|---|---|
| `--out DIR` | Where to write. Overrides the destination stored in the project. |
| `--format FORMAT` | `png`, `jpeg` or `webp`. |
| `--quality N` | 1–100, for jpeg and webp. |
| `--scale N` | Cap the width at N pixels. Never enlarges. |
| `--original` | Ignore any downscale the project asks for. |
| `--only NAMES` | Comma-separated cut or screen names. |
| `--dry-run` | Print what would be written, write nothing. |
| `--json` | Machine-readable output, for tools. |
| `--font-dir DIR` | Look here for fonts first. Repeatable. |

Exit codes: `0` success, `1` failure, `2` the project file could not be read or
is invalid. `validate` is the one to put in a pre-commit hook.

```bash
refigure list                          # what would be written, and at what size
refigure export --format jpeg --quality 82 --scale 1200
refigure export --only login,checkout  # two cuts, by name
refigure export --dry-run --json       # for a script to read
```

## In CI

```yaml
- name: Regenerate tutorial images
  run: |
    curl -sSL https://github.com/oduvan/refigure-cli/releases/latest/download/refigure_${VERSION}_linux_amd64.tar.gz | tar -xz
    ./refigure export ./docs/screenshots --out ./site/static/img --scale 1400
    ./refigure validate ./docs/screenshots
```

Fonts are the one thing to get right on a build machine — see below.

## The project file

`refigure.yaml` is written by the desktop app, and is meant to be readable and
diffable by hand:

```yaml
version: 1
name: onboarding
style:
  color: '#D93A3E'
  stroke: { width: 3, style: solid }
  font: { family: Inter, size: 15 }
export:
  dest: ../site/static/img
  format: png
  quality: 90
  scale: original
screens:
  - id: scr_a1b2
    name: connect
    file: connect.png
    width: 1200
    height: 600
    cuts:
      - id: cut_c3d4
        name: connect-token
        rect: { x: 40, y: 60, w: 700, h: 380 }
    figures:
      - id: fig_e5f6
        type: rect
        cut: cut_c3d4          # owned by that cut: appears there and nowhere else
        rect: { x: 80, y: 120, w: 220, h: 44 }
      - id: fig_g7h8
        type: arrow            # unowned: appears in every cut it overlaps
        from: { x: 300, y: 200 }
        to: { x: 520, y: 300 }
        style: { color: '#1D9A6C' }
```

Two rules decide what a cut contains, and both are worth knowing before you read
an export:

- **Figures are stored in screen coordinates, never cut coordinates** — the
  exporter translates them.
- **A figure with `cut:` belongs to that cut alone.** A figure without one
  appears in every cut whose rectangle it overlaps. `cuts[].figures.exclude`
  overrides either.

Style resolves through a cascade: built-in default → project `style:` →
screen `style:` → the figure's own `style:`.

Unknown keys are ignored on purpose, so the desktop app can add settings without
this tool needing a matching release.

## Fonts

Text figures are drawn with a real font file, found by family name in the
system font directories (and in any `--font-dir` you pass). If the family is
missing, the tool says so and falls back to a bundled font — the image is still
written, but the text will not match what the editor showed.

Build machines usually have no fonts at all. Either install the family you use,
or commit the `.ttf` next to your project and pass `--font-dir`.

## Limits

- **WebP output is not supported yet.** Every Go WebP encoder needs cgo, which
  would cost the single static binary. Reading `.webp` screenshots works.
- **Downscaling is not pixel-identical to the desktop app.** This tool uses
  Catmull-Rom; the desktop uses sharp's Lanczos3. Sharp edges can differ by a
  hair at the same size.

## Build

```bash
make build       # ./refigure
make check       # gofmt, go vet, go test -race
make build-all   # every release target into ./dist
```

Go 1.23 or newer. `CGO_ENABLED=0` everywhere.

## License

MIT — see [LICENSE](LICENSE).

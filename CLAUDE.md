# CLAUDE.md

Working notes for `refigure` — the command-line exporter for
[Refigure](https://github.com/oduvan/refigure) projects.

The desktop app creates and maintains a project. This tool turns that project
into images, with no window and no Electron. It is a single static binary so it
can be dropped into CI: regenerate the tutorial images on every push, and the
screenshots in your docs stop going stale.

## The two repositories

| Repo | What it owns |
|---|---|
| [oduvan/refigure](https://github.com/oduvan/refigure) | The desktop app, and **the specification**: `OVERVIEW.md`, `DESIGN.md`, `TECH_STACK.md`, `GLOSSARY.md`. It writes `refigure.yaml`. |
| this one | Reading that file and drawing the images. |

**The specification lives in the other repo, and that repo wins.** The format,
the semantics of ownership and membership, the style cascade, the default style
— all decided there. If this tool disagrees with the desktop app, this tool is
wrong, unless the MD documents say otherwise. Use the vocabulary in the other
repo's `GLOSSARY.md`: *screen*, *screenshot*, *cut*, *figure*, *ownership*,
*membership*, *exclusion*, *cascade*, *export plan*, *downscale*. Do not invent a
second name for something that has one (*region* for cut, *shape* for figure).

## Commands

```bash
make build       # ./refigure
make check       # gofmt check, go vet, go test -race
make test
make build-all   # every release target into ./dist
```

Go 1.23+. `CGO_ENABLED=0` everywhere — see the invariants.

## Architecture

```
cmd/refigure/main.go    flags, subcommands, output (text and --json), exit codes
internal/
├── format/   reads refigure.yaml (project.go) and resolves the style cascade (style.go)
├── geom/     rect maths, figure bounds, membership
├── export/   plan.go: what to write and under which name · write.go: encoding
└── render/   render.go: draws a cut · font.go: finds a font by family name
```

The dependency direction is `main → export → render → format/geom`. Nothing in
`internal/` reads flags or prints; `main` does all the talking.

## Invariants worth preserving

- **This tool never writes `refigure.yaml`.** It reads the project and writes
  images. That is why none of the desktop app's careful serialisation —
  deterministic key order, comments kept with their entity by id — exists here,
  and why it must not be added.
- **Unknown keys are ignored on purpose.** It lets the desktop app add fields,
  or move its own settings into a separate file, without this tool needing a
  matching release. Never switch the YAML decoder to strict mode.
- **The drawing constants mirror the desktop's `FigureShape.tsx`, exactly.**
  Dash pattern `[width*3, width*2]`; arrow head `pointerLength` and
  `pointerWidth` both `max(8, width*3)`; round line caps and joins; rectangle
  `cornerRadius` 2, never filled; text at weight 600 with `lineHeight` 1.25 and
  the canvas "middle" baseline. `format.DefaultStyle` mirrors the desktop's
  `DEFAULT_STYLE` — `#D93A3E`, stroke 3 solid, Inter 15. Changing any of these
  on one side alone makes the two renderers disagree, which is the one bug this
  project cannot tolerate. Change the specification first.
- **Figures are in screen coordinates, never cut coordinates.** `render.Cut`
  translates the whole scene by `-rect.X, -rect.Y` and then draws figures at
  their stored coordinates. Do not pre-subtract anywhere else.
- **Ownership beats overlap.** A figure with `cut:` renders in that cut alone;
  one without renders in every cut it overlaps; `cuts[].figures.exclude` beats
  both. `geom.Includes` is the only place that decides this — one function, so
  it can be compared against the desktop's `cutIncludesFigure` by eye.
- **Downscale never enlarges.** A `--scale` wider than the cut is ignored, not
  applied. `export.Plan` decides the size; `render.Resize` only obeys.
- **No cgo, ever.** The point of the binary is that it runs on a build machine
  with nothing installed. This is why WebP output is missing: every Go WebP
  encoder needs cgo. Decoding `.webp` screenshots is pure Go and works.
- **Stdlib first.** Four dependencies, all pure Go: `fogleman/gg` (rasteriser),
  `golang/freetype` (TrueType faces), `golang.org/x/image` (resampling, WebP
  decode, the fallback font), `gopkg.in/yaml.v3`. Adding a fifth needs a reason.

## Where the two renderers can drift

The desktop draws with Konva in a browser; this tool draws with `gg` on a CPU.
They agree on geometry because the constants are copied. Two places they do not:

- **Text is the weak point.** The browser has a font stack, a shaper and its own
  hinting; here a `.ttf` is found by family name and rendered by freetype. A
  missing family is reported through `Options.OnMissingFont` and the CLI warns,
  because silently drawing the wrong font is worse than an ugly warning.
  `geom` approximates text width at `0.55 × size` per character — deliberately
  the same crude approximation the desktop uses, so both agree about *membership*
  even when they disagree about pixels.
- **Resampling.** `render.Resize` uses Catmull-Rom; the desktop uses sharp's
  Lanczos3. Close, not identical, on sharp edges.

Neither is worth chasing with cleverness. The way to hold them together is
golden-image fixtures shared between the repos — that is
[refigure#9](https://github.com/oduvan/refigure/issues/9), and it should land
before the desktop app starts calling this binary instead of rendering exports
itself.

## Testing

`make test` — 47 tests, no fixtures on disk; every test builds its own project
or image. **Run it through `make`, not as a bare `go test ./...`**: see the
caching note at the end of this section.

- `internal/format` — a hand-written project file, unknown fields ignored,
  `scale` as a number or `original`, a broken document reporting its line, a
  missing file, a nameless cut, a newer format version.
- `internal/geom` — normalising, arrow bounds, membership, ownership overriding
  overlap, the shared text approximation, multiline text, exclusion.
- `internal/export` — ownership and exclusion end to end, downscale never
  enlarging, extensions, name collisions, `--only`, zero-sized cuts, the cascade.
- `internal/render` — real pixels: the cut crops to its rectangle, a figure at
  screen (60,50) lands at (10,10) inside a cut starting at (50,40), an arrow
  head is wider than its shaft, a dashed line leaves gaps, a missing font is
  reported, a bad colour is an error.
- `cmd/refigure` — the command surface, driven as a subprocess: exit codes (2
  for an unreadable project, 1 for a failure), the line number in a broken file,
  `--out`, `--only`, `--dry-run` writing nothing, `--json` naming files that are
  really there, `--format`/`--quality` producing a decodable JPEG, `--scale`
  downscaling and refusing to enlarge, a missing font warning without failing,
  and WebP failing with a message that names the format.

**The command tests build the binary inside `TestMain`, so `go test` cannot see
what they depend on and will serve a cached pass after a real change.** That was
confirmed by mutation, not assumed: breaking the exit code passes under a bare
`go test ./...` and fails under `-count=1`. The Makefile and CI both pass
`-count=1`; do not remove it, and do not trust a bare `go test` in this repo.

CI runs the tests on Linux, macOS and Windows, because font resolution and
rasterising are not identical across them, and cross-compiles all six release
targets on every push so a broken target is found before a tag is cut.

## Releasing

Tag and push:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

`.github/workflows/release.yml` builds darwin/linux/windows × amd64/arm64,
packs each with the README and LICENSE, writes `checksums.txt`, and creates the
GitHub release with generated notes. `main.Version` comes from the tag via
`-ldflags`. The binaries are not signed or notarised; the README tells macOS
users how to get past Gatekeeper.

## Open points

- **The config split has not happened yet.** The plan is two files in the
  project folder: one holding everything needed to export, one holding what only
  the desktop app cares about, with no overlap. Today there is one
  `refigure.yaml` and this tool ignores the keys that are not its business —
  which is why the split can happen later without breaking anything here.
  Tracked as [refigure#35](https://github.com/oduvan/refigure/issues/35).
- **The desktop app still renders its own exports.** It does not call this
  binary yet; that needs the binary shipped inside the app bundle. Until then
  the two renderers are genuinely two implementations, and the fidelity notes
  above are the risk register. Tracked as
  [refigure#34](https://github.com/oduvan/refigure/issues/34), which also has to
  decide what happens to WebP.
- **WebP output** returns a clear error rather than pretending. Revisit only if
  a pure-Go encoder appears.

## Conventions

- `gofmt`, and CI fails without it.
- Comments explain *why*, not what. The interesting comments here are the ones
  that record a decision the reader would otherwise undo — the copied constants,
  the crude text width, Catmull-Rom, ignoring unknown keys.
- Errors are sentences a user can act on, lower case, no stack traces. A project
  that cannot be read exits `2`, not `1`, so a script can tell the difference.
- No global state, no `init()` side effects. `main` is the only package that may
  print or exit.

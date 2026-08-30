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

**Homebrew** (macOS and Linux):

```bash
brew tap oduvan/refigure https://github.com/oduvan/refigure-cli
brew install refigure
```

`brew upgrade refigure` from then on.

**npm** — for a docs project that already has Node:

```bash
npx refigure-cli export ./docs/screenshots --out ./site/static/img
```

Installing `refigure-cli` downloads one binary for your platform. There is no
postinstall script and nothing is fetched afterwards, so it works behind a proxy
and with `--ignore-scripts`. Releases are published from CI with npm trusted
publishing, so every version carries provenance and no token exists to leak.

**Or download one file.** Take a binary for your platform from
[Releases](https://github.com/oduvan/refigure-cli/releases), unpack it, and put
`refigure` somewhere on your `PATH`.

```bash
# macOS (Apple silicon) — swap the platform for yours
curl -sSL https://github.com/oduvan/refigure-cli/releases/download/v0.1.7/refigure_v0.1.7_darwin_arm64.tar.gz | tar -xz
sudo mv refigure /usr/local/bin/
```

macOS may refuse the binary the first time because it is not notarised: open
System Settings → Privacy & Security and allow it, or run
`xattr -d com.apple.quarantine refigure`.

With a Go toolchain:

```bash
go install github.com/oduvan/refigure-cli/cmd/refigure@latest
```

A binary installed this way reports the module version it came from, so
`refigure version` still answers usefully.

## Keeping it up to date

Whichever way it was installed, `refigure version` says what you have, and the
[releases page](https://github.com/oduvan/refigure-cli/releases) says what is
current. Downloaded binaries do not update themselves.

**In CI, pin a version.** `latest` means the exporter can change under a build
that has not changed, and the images with it. Pin the version you tested with
and move it deliberately.

## Use

```
refigure export   [project] [flags]   write one image per cut
refigure list     [project] [flags]   what would be written, and at what size
refigure validate [project] [flags]   check refigure.yaml
refigure schema            [flags]    describe the project file format
refigure version
refigure help [command]
```

Every command takes `--help` and `--json`.

The project defaults to the current directory.

| Export flag | What it does |
|---|---|
| `--out DIR` | Where to write. Overrides the destination stored in the project. |
| `--format FORMAT` | `png`, `jpeg` or `webp`. |
| `--quality N` | 1–100, for jpeg and webp. |
| `--scale N` | Cap the width at N pixels. Never enlarges. |
| `--original` | Ignore any downscale the project asks for. |
| `--only NAMES` | Comma-separated cut or screen names. |
| `--only-id IDS` | Comma-separated cut ids, for tools that must be exact. |
| `--dry-run` | Print what would be written, write nothing. |
| `--json` | Machine-readable output, for tools. |
| `--progress` | Report each image on stderr as it is written. |
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

There is nothing to install and no action to add — it is one binary, so the
same four lines work on GitHub Actions, GitLab CI, Jenkins or a cron job:

```yaml
- name: Regenerate tutorial images
  env:
    REFIGURE_VERSION: v0.1.7
  run: |
    set -euo pipefail
    asset="refigure_${REFIGURE_VERSION}_linux_amd64.tar.gz"
    base="https://github.com/oduvan/refigure-cli/releases/download/${REFIGURE_VERSION}"
    curl -sSLO "$base/$asset"
    curl -sSLO "$base/checksums.txt"
    grep " $asset\$" checksums.txt | sha256sum -c -
    tar -xzf "$asset" refigure

    ./refigure validate ./docs/screenshots
    ./refigure export   ./docs/screenshots --out ./site/static/img --scale 1400
```

Three things in there are deliberate:

- **The version is pinned.** `latest` means the exporter can change under a
  build where nothing else changed, and your images with it. Move the pin when
  you have looked at the diff.
- **The download is checked** against the checksums published with the release,
  which is the same thing the desktop app does before it runs a binary.
- **`validate` runs first**, so a broken project file fails the job in a second
  rather than after the images are written.

Fonts are the one thing to get right on a build machine — see below.

## Writing a project file

The tool describes its own format, so a script — or an agent holding only the
binary — never has to guess:

```bash
refigure schema             # every key, what it means, and the two rules
refigure schema --example   # a complete refigure.yaml that validates
refigure schema --json      # the same as a JSON Schema
```

Then check what you wrote. `validate` reports **every** problem at once, with
the line each is on:

```
warning: line 4: unknown key `colour` in `style` — it is ignored
       did you mean `color`?
error: line 24: figure "fig_box" belongs to cut "cut_missing", which does not exist
       an owned figure appears only in its own cut, so this one appears nowhere
```

`--json` gives the same as
`{"ok":false,"screens":1,"cuts":2,"problems":[{"severity","message","line","hint"}]}`
— on stdout whether it passed or not, so there is one thing to parse. `--strict`
makes warnings fail too.

It catches what a reader of the format cannot: a key nobody read (the loader
ignores unknown keys on purpose, which is exactly what makes a typo invisible),
an id used twice, a figure owned by a cut that does not exist and therefore
appears nowhere, two cuts whose images would overwrite each other, a missing
screenshot, a cut with no area or one running off the edge.

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

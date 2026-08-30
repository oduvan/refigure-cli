# refigure-cli

Export a [Refigure](https://github.com/oduvan/refigure) project to images.

```bash
npx refigure-cli export ./docs/screenshots --out ./site/static/img
```

A Refigure project is a folder: one `refigure.yaml` and the screenshots it
references. Annotations live in the YAML as data, never baked into pixels. This
writes one image per cut, so tutorial images can be rebuilt on every push and
never drift from the project they came from.

The tool describes its own format, so nothing has to guess:

```bash
npx refigure-cli schema             # the format, explained
npx refigure-cli schema --example   # a complete file that validates
npx refigure-cli validate --json    # every mistake at once, with line numbers
```

It reads your project and writes images. It never writes the project file.

Installing this package downloads one binary for your platform — nothing is
fetched afterwards, and there is no postinstall script.

Full documentation: <https://github.com/oduvan/refigure-cli>

MIT licensed.

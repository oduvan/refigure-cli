package format

// The project file, described by the tool that reads it.
//
// This exists because the most likely caller is a program, not a person: an
// agent holding only the binary has no repository to read, and guessing at a
// file format is how invalid projects get written. `refigure schema` prints
// this; `refigure schema --json` prints the JSON Schema; `refigure schema
// --example` prints a complete file that validates.
//
// It is kept beside the types it describes so the two move together.

// SchemaReference is the human-readable description of refigure.yaml.
const SchemaReference = `refigure.yaml — the project file

A project is a folder holding refigure.yaml and the screenshots it references.
Paths inside the file are relative to that folder. Unknown keys are ignored, so
a file written by a newer desktop app still exports here.

Top level
  version   int      required. Format version. This build understands 1.
  name      string   required. The project's name.
  style     Style    optional. Defaults for every figure in the project.
  export    Export   optional. Defaults for the export command.
  screens   [Screen] optional. Empty means there is nothing to export.

Screen — one source screenshot, and the work done on it
  id        string   required. Stable and unique. Never reused.
  name      string   required. Used by --only.
  file      string   required. Image file name, relative to the project folder.
  width     int      optional. Pixel size of the screenshot, for reference.
  height    int      optional.
  style     Style    optional. Overrides the project style for this screen.
  cuts      [Cut]    optional.
  figures   [Figure] optional. In SCREEN coordinates, never cut coordinates.

Cut — a named rectangle that exports as one image
  id        string   required. Stable and unique.
  name      string   required. Becomes the file name: name + "." + format.
  rect      Rect     required. Position and size on the screen.
  figures   object   optional. { exclude: [figure id, ...] } hides figures here.

Figure — something drawn on the screenshot
  id        string   required.
  type      string   required. One of: arrow, line, rect, text.
  cut       string   optional. A cut id. The figure then belongs to that cut
                     alone and moves with it. Without it the figure appears in
                     every cut whose rectangle it overlaps.
  style     Style    optional. Overrides everything above it.

  arrow, line need:  from: {x, y}   to: {x, y}
  rect needs:        rect: {x, y, w, h}
  text needs:        at: {x, y}     text: "the words"

Rect      { x, y, w, h }   numbers, in screen pixels. w and h are not negative.
Point     { x, y }         numbers, in screen pixels.

Style — every field optional; missing fields come from the level above
  color            string   "#RRGGBB".
  stroke.width     number   1 to 12.
  stroke.style     string   solid or dashed.
  font.family      string   a font installed on this machine, or --font-dir.
  font.size        number   6 to 200.

  Resolution order: built-in default, then project style, then screen style,
  then the figure's own style. Built-in default is
  #D93A3E, stroke 3 solid, Inter 15.

Export — defaults for the export command; every flag overrides these
  dest      string   output folder, relative to the project folder or absolute.
  format    string   png, jpeg or webp. Default png.
  quality   int      1 to 100, for jpeg and webp. Default 90.
  scale     mixed    "original", or a maximum width in pixels. Never enlarges.

Two rules decide what a cut contains
  1. Figures are stored in screen coordinates. The exporter translates them.
  2. A figure with cut: belongs to that cut alone. A figure without one appears
     in every cut it overlaps. cuts[].figures.exclude overrides either.
`

// SchemaExample is a complete project file that validates.
const SchemaExample = `version: 1
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
      - id: cut_e5f6
        name: connect-full
        rect: { x: 0, y: 0, w: 1200, h: 600 }
        figures:
          exclude: [fig_note]
    figures:
      - id: fig_box
        type: rect
        cut: cut_c3d4
        rect: { x: 80, y: 120, w: 220, h: 44 }
      - id: fig_arrow
        type: arrow
        from: { x: 300, y: 200 }
        to: { x: 520, y: 300 }
        style: { color: '#1D9A6C' }
      - id: fig_note
        type: text
        at: { x: 80, y: 420 }
        text: Paste the token here
`

// SchemaJSON is the JSON Schema for refigure.yaml, for a caller that would
// rather check a file itself than shell out to validate.
const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/oduvan/refigure-cli/refigure.yaml",
  "title": "Refigure project file",
  "type": "object",
  "required": ["version", "name"],
  "properties": {
    "version": { "type": "integer", "minimum": 1, "maximum": 1 },
    "name": { "type": "string", "minLength": 1 },
    "style": { "$ref": "#/$defs/style" },
    "export": {
      "type": "object",
      "properties": {
        "dest": { "type": "string" },
        "format": { "enum": ["png", "jpeg", "webp"] },
        "quality": { "type": "integer", "minimum": 1, "maximum": 100 },
        "scale": {
          "oneOf": [{ "const": "original" }, { "type": "integer", "minimum": 1 }]
        }
      }
    },
    "screens": { "type": "array", "items": { "$ref": "#/$defs/screen" } }
  },
  "$defs": {
    "point": {
      "type": "object",
      "required": ["x", "y"],
      "properties": { "x": { "type": "number" }, "y": { "type": "number" } }
    },
    "rect": {
      "type": "object",
      "required": ["x", "y", "w", "h"],
      "properties": {
        "x": { "type": "number" },
        "y": { "type": "number" },
        "w": { "type": "number", "minimum": 0 },
        "h": { "type": "number", "minimum": 0 }
      }
    },
    "style": {
      "type": "object",
      "properties": {
        "color": { "type": "string", "pattern": "^#[0-9a-fA-F]{6}$" },
        "stroke": {
          "type": "object",
          "properties": {
            "width": { "type": "number", "minimum": 1, "maximum": 12 },
            "style": { "enum": ["solid", "dashed"] }
          }
        },
        "font": {
          "type": "object",
          "properties": {
            "family": { "type": "string", "minLength": 1 },
            "size": { "type": "number", "minimum": 6, "maximum": 200 }
          }
        }
      }
    },
    "screen": {
      "type": "object",
      "required": ["id", "name", "file"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "name": { "type": "string", "minLength": 1 },
        "file": { "type": "string", "minLength": 1 },
        "width": { "type": "integer", "minimum": 1 },
        "height": { "type": "integer", "minimum": 1 },
        "style": { "$ref": "#/$defs/style" },
        "cuts": { "type": "array", "items": { "$ref": "#/$defs/cut" } },
        "figures": { "type": "array", "items": { "$ref": "#/$defs/figure" } }
      }
    },
    "cut": {
      "type": "object",
      "required": ["id", "name", "rect"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "name": { "type": "string", "minLength": 1 },
        "rect": { "$ref": "#/$defs/rect" },
        "figures": {
          "type": "object",
          "properties": {
            "exclude": { "type": "array", "items": { "type": "string" } }
          }
        }
      }
    },
    "figure": {
      "type": "object",
      "required": ["id", "type"],
      "properties": {
        "id": { "type": "string", "minLength": 1 },
        "type": { "enum": ["arrow", "line", "rect", "text"] },
        "cut": { "type": "string", "minLength": 1 },
        "style": { "$ref": "#/$defs/style" },
        "from": { "$ref": "#/$defs/point" },
        "to": { "$ref": "#/$defs/point" },
        "at": { "$ref": "#/$defs/point" },
        "rect": { "$ref": "#/$defs/rect" },
        "text": { "type": "string" }
      },
      "allOf": [
        {
          "if": { "properties": { "type": { "enum": ["arrow", "line"] } } },
          "then": { "required": ["from", "to"] }
        },
        {
          "if": { "properties": { "type": { "const": "rect" } } },
          "then": { "required": ["rect"] }
        },
        {
          "if": { "properties": { "type": { "const": "text" } } },
          "then": { "required": ["at", "text"] }
        }
      ]
    }
  }
}
`

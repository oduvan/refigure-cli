// `refigure mcp` — the same jobs the command line offers, offered to an agent.
//
// The audience is the reason this exists. An agent editing refigure.yaml has no
// way to see what it wrote: the pixels are the product, and a file it cannot
// look at is a file it cannot check. So the tools here are the command line's,
// plus one the command line has no use for — `preview` hands a rendered cut
// back as an image.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/oduvan/refigure-cli/internal/export"
	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/mcp"
)

const mcpUsage = `refigure mcp [project] — serve the Model Context Protocol on stdin and stdout

Offers an agent the jobs this tool does: describe the project file format,
check a project, list what an export would write, write it, and look at one cut
as an image.

It speaks over the standard streams, so a client starts it as a subprocess
rather than connecting to it. In a client's configuration:

  {
    "mcpServers": {
      "refigure": { "command": "refigure", "args": ["mcp", "/path/to/project"] }
    }
  }

Tools:
  schema     the format: prose, a complete example, or a JSON Schema
  validate   every problem in a project file at once, with line numbers
  list       what an export would write, and at what size
  export     write one image per cut
  preview    render one cut and hand it back as an image

The project folder named here is what every tool uses unless a call names a
different one, and it defaults to the current directory.

This server never writes refigure.yaml. Editing the project is the desktop
app's job, or the agent's; drawing the images is this one's.

Nothing but protocol messages reaches stdout — the transport requires that.
There is nothing to configure and no port to open; end the server by closing
its input.
`

// mcpInstructions is what a client may put in front of the model before it sees
// the tools. It carries the two rules that cannot be guessed from the tool
// names, which is the same job `schema`'s prose does for the file format.
const mcpInstructions = `Refigure keeps tutorial screenshots maintainable. The annotations — arrows,
rectangles, lines, text, and blurred or pixelated regions — live in refigure.yaml
as data rather than being baked into the pixels, so a change to the product
means adjusting a file rather than retaking every picture. A project is a folder
holding that file and the screenshots it names; each cut in it becomes one
exported image.

These tools draw images from a project. They never write refigure.yaml: edit it
yourself, then call validate, which reports every problem at once with the line
each one is on. Call schema first if you have not written one before — unknown
keys are ignored on purpose, so a misspelled key does nothing at all until
validate points at it.

Two rules decide what an exported image contains, and neither can be guessed
from the keys. Figures are stored in screen coordinates, never coordinates
relative to the cut. And a figure with a cut: of its own belongs to that cut
alone, while a figure without one appears in every cut its shape overlaps.`

// Big enough to read a UI in, small enough that base64 does not swamp the
// answer. A preview is for checking a change, not for shipping.
const (
	defaultPreviewWidth = 900
	maxPreviewWidth     = 2000
)

func runMCP(args []string) int {
	if wantsHelp(args) {
		fmt.Print(mcpUsage)
		return 0
	}
	flags := flag.NewFlagSet("mcp", flag.ExitOnError)
	dir := parseDir(flags, args)

	server := &mcp.Server{
		Name:         "refigure",
		Version:      reportedVersion(),
		Instructions: mcpInstructions,
		Tools:        mcpTools(dir),
	}
	if err := server.Serve(os.Stdin, os.Stdout); err != nil {
		return fail(err)
	}
	return 0
}

// callArgs is every argument any of the tools takes. One struct rather than
// five because each tool's own schema is the contract a client checks against;
// this is only what gets read once it has.
type callArgs struct {
	Project  string   `json:"project"`
	Form     string   `json:"form"`
	Out      string   `json:"out"`
	Format   string   `json:"format"`
	Quality  count    `json:"quality"`
	Scale    count    `json:"scale"`
	Original bool     `json:"original"`
	Only     []string `json:"only"`
	OnlyID   []string `json:"onlyId"`
	DryRun   bool     `json:"dryRun"`
	Cut      string   `json:"cut"`
	MaxWidth count    `json:"maxWidth"`
}

// count is a whole-number argument that also accepts the way a model is just as
// likely to write one. JSON Schema calls 100.0 an integer, so a client checking
// a call against the schema this server published will pass it straight
// through — and Go will not put it in an int. Refusing what we said we accept
// is the server's mistake, not the model's.
type count int

func (c *count) UnmarshalJSON(data []byte) error {
	var number float64
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*c = count(number)
	return nil
}

func mcpTools(defaultDir string) []mcp.Tool {
	// Which folder a call means. Naming one is optional so a client configured
	// for a single project does not have to repeat it, and so a model working
	// in that project cannot point a write somewhere else by getting a relative
	// path wrong.
	folder := func(args callArgs) string {
		if args.Project != "" {
			return args.Project
		}
		return defaultDir
	}

	project := mcp.Property{
		Name: "project", Type: "string",
		Description: "The project folder: it holds refigure.yaml and the screenshots. Defaults to the folder this server was started in.",
	}

	return []mcp.Tool{
		{
			Name:  "schema",
			Title: "The project file format",
			Description: "Describe refigure.yaml — every key, what it means, and the two rules that decide which figures a cut contains. " +
				"Read this before writing or editing a project file: unknown keys are ignored rather than refused, so a guess that is wrong does nothing and says nothing.",
			Arguments: []mcp.Property{{
				Name: "form", Type: "string",
				Description: "reference for the prose (the default), example for a complete refigure.yaml that validates, json for the JSON Schema.",
				Enum:        []string{"reference", "example", "json"},
			}},
			Annotations: mcp.Annotations{ReadOnly: true, Idempotent: true},
			Call: func(raw json.RawMessage) (mcp.Result, error) {
				args, err := decodeArgs(raw)
				if err != nil {
					return mcp.Result{}, err
				}
				switch args.Form {
				case "", "reference":
					return said(format.SchemaReference), nil
				case "example":
					return said(format.SchemaExample), nil
				case "json":
					return said(format.SchemaJSON), nil
				}
				return mcp.Result{}, fmt.Errorf("form is reference, example or json — not %q", args.Form)
			},
		},

		{
			Name:  "validate",
			Title: "Check a project file",
			Description: "Check refigure.yaml and report every problem at once, with the line each one is on. " +
				"Errors are things that make an export fail or produce an image the file does not describe; warnings are things that export but are almost certainly mistakes, a key nobody read among them. " +
				"Call this after every edit to a project file.",
			Arguments:   []mcp.Property{project},
			Annotations: mcp.Annotations{ReadOnly: true, Idempotent: true},
			Call: func(raw json.RawMessage) (mcp.Result, error) {
				args, err := decodeArgs(raw)
				if err != nil {
					return mcp.Result{}, err
				}
				dir := folder(args)
				result := performValidate(dir)
				return mcp.Result{
					Content:    []mcp.Content{mcp.Text("%s", validationText(dir, result))},
					Structured: result,
					// A file that does not validate is the model's to fix, so it
					// is an error it can read rather than one the client swallows.
					IsError: !result.OK,
				}, nil
			},
		},

		{
			Name:        "list",
			Title:       "What an export would write",
			Description: "List the images an export would write from this project: one per cut, with the file name and the size after any downscale the project asks for. Writes nothing and creates nothing.",
			Arguments:   []mcp.Property{project},
			Annotations: mcp.Annotations{ReadOnly: true, Idempotent: true},
			Call: func(raw json.RawMessage) (mcp.Result, error) {
				args, err := decodeArgs(raw)
				if err != nil {
					return mcp.Result{}, err
				}
				plan, err := performList(folder(args))
				if err != nil {
					return mcp.Result{}, err
				}
				result := planJSON("", plan, nil)

				var text strings.Builder
				for _, item := range plan.Items {
					fmt.Fprintf(&text, "%s\t%s\t%dx%d\n", item.Screen.Name, item.FileName, item.Width, item.Height)
				}
				if len(plan.Items) == 0 {
					text.WriteString("this project has no cuts, so an export would write nothing")
				}
				return mcp.Result{
					Content:    []mcp.Content{mcp.Text("%s", text.String())},
					Structured: result,
				}, nil
			},
		},

		{
			Name:  "export",
			Title: "Write the images",
			Description: "Write one image per cut. Each file is named after its cut, so two cuts with the same name overwrite each other and an existing file of that name is replaced. " +
				"Pass dryRun to find out what it would write without writing it. " +
				"With no out, the destination stored in the project is used; a relative one resolves against the project folder, not the working directory.",
			Arguments: []mcp.Property{
				project,
				{Name: "out", Type: "string", Description: "Where to write. Overrides the destination stored in the project. A relative path resolves against the project folder."},
				{Name: "format", Type: "string", Description: "Overrides the format stored in the project.", Enum: []string{"png", "jpeg", "webp"}},
				{Name: "quality", Type: "integer", Description: "1-100, for jpeg and webp. Overrides the project's setting."},
				{Name: "scale", Type: "integer", Description: "Cap the width at this many pixels. Never enlarges: a cut narrower than this is written at its own size."},
				{Name: "original", Type: "boolean", Description: "Ignore any downscale the project asks for."},
				{Name: "only", Type: "string[]", Description: "Export only these cut or screen names. Names repeat across screens; use onlyId to mean one cut exactly."},
				{Name: "onlyId", Type: "string[]", Description: "Export only the cuts with these ids."},
				{Name: "dryRun", Type: "boolean", Description: "Report what would be written and write nothing."},
			},
			// Not read-only, and it replaces files rather than adding to them —
			// which is what a client needs to know before it decides whether to
			// ask a person first.
			Annotations: mcp.Annotations{Destructive: true, Idempotent: true},
			Call: func(raw json.RawMessage) (mcp.Result, error) {
				args, err := decodeArgs(raw)
				if err != nil {
					return mcp.Result{}, err
				}

				var warnings []string
				outcome, err := performExport(exportRequest{
					Dir:      folder(args),
					Out:      args.Out,
					Format:   args.Format,
					Quality:  int(args.Quality),
					Scale:    int(args.Scale),
					Original: args.Original,
					Only:     args.Only,
					OnlyIDs:  args.OnlyID,
					DryRun:   args.DryRun,
				}, func(message string) { warnings = append(warnings, message) }, nil)
				if err != nil {
					return mcp.Result{}, err
				}

				report := exportReport{output: planJSON(outcome.Dest, outcome.Plan, outcome.Written), Warnings: warnings}
				report.DryRun = outcome.DryRun

				var text strings.Builder
				if outcome.DryRun {
					fmt.Fprintf(&text, "would write %d image(s) to %s:\n", len(outcome.Plan.Items), outcome.Dest)
					for _, item := range outcome.Plan.Items {
						fmt.Fprintf(&text, "  %s\t%dx%d\n", item.FileName, item.Width, item.Height)
					}
				} else {
					fmt.Fprintf(&text, "wrote %d image(s) to %s:\n", len(outcome.Written), outcome.Dest)
					for _, name := range outcome.Written {
						fmt.Fprintf(&text, "  %s\n", name)
					}
				}
				for _, warning := range warnings {
					fmt.Fprintf(&text, "warning: %s\n", warning)
				}
				return mcp.Result{Content: []mcp.Content{mcp.Text("%s", text.String())}, Structured: report}, nil
			},
		},

		{
			Name:  "preview",
			Title: "Look at one cut",
			Description: "Render one cut and return it as an image, so you can see what the annotations actually look like. Writes nothing to disk. " +
				"Name the cut by its name or its id; a project with exactly one cut needs neither.",
			Arguments: []mcp.Property{
				project,
				{Name: "cut", Type: "string", Description: "The cut to draw, by name or by id. Optional when the project has only one."},
				{Name: "maxWidth", Type: "integer", Description: fmt.Sprintf("Cap the preview's width, in pixels. Defaults to %d and never exceeds %d. Never enlarges.", defaultPreviewWidth, maxPreviewWidth)},
			},
			Annotations: mcp.Annotations{ReadOnly: true, Idempotent: true},
			Call: func(raw json.RawMessage) (mcp.Result, error) {
				args, err := decodeArgs(raw)
				if err != nil {
					return mcp.Result{}, err
				}
				return preview(folder(args), args.Cut, int(args.MaxWidth))
			},
		},
	}
}

// exportReport is what the export tool returns as data: the same document
// `refigure export --json` prints, and the warnings that went to stderr there.
// A model has no stderr to read, and "the font you asked for is not in this
// build" is exactly the kind of thing it needs told.
type exportReport struct {
	output
	Warnings []string `json:"warnings,omitempty"`
}

func preview(dir, wanted string, maxWidth int) (mcp.Result, error) {
	project, err := openProject(dir)
	if err != nil {
		return mcp.Result{}, err
	}

	width := maxWidth
	if width <= 0 {
		width = defaultPreviewWidth
	}
	if width > maxPreviewWidth {
		width = maxPreviewWidth
	}

	// PNG regardless of what the project exports as: this is a picture to look
	// at once, and a lossy one would show artefacts the real export has not got.
	plan, err := export.Build(project, export.Options{MaxWidth: width, Format: format.FormatPNG})
	if err != nil {
		return mcp.Result{}, err
	}
	item, err := pickCut(plan, wanted)
	if err != nil {
		return mcp.Result{}, err
	}

	var warnings []string
	img, err := renderItem(project, item, map[string]image.Image{}, map[string]bool{},
		func(message string) { warnings = append(warnings, message) })
	if err != nil {
		return mcp.Result{}, err
	}

	var encoded bytes.Buffer
	if err := export.EncodeTo(&encoded, img, format.FormatPNG, 100); err != nil {
		return mcp.Result{}, fmt.Errorf("cut %q: %w", item.Cut.Name, err)
	}

	caption := fmt.Sprintf("cut %q (%s) of screen %q, %d×%d in the project, shown here at %d×%d",
		item.Cut.Name, item.Cut.ID, item.Screen.Name, int(item.Rect.W), int(item.Rect.H), item.Width, item.Height)
	for _, warning := range warnings {
		caption += "\nwarning: " + warning
	}

	return mcp.Result{Content: []mcp.Content{
		mcp.Text("%s", caption),
		mcp.Image(encoded.Bytes(), "image/png"),
	}}, nil
}

// pickCut finds the one cut a preview is about. Being told which cuts there are
// is the useful half of being told the name was wrong.
func pickCut(plan *export.Plan, wanted string) (export.Item, error) {
	if len(plan.Items) == 0 {
		return export.Item{}, fmt.Errorf("this project has no cuts to draw")
	}
	if wanted == "" {
		if len(plan.Items) == 1 {
			return plan.Items[0], nil
		}
		return export.Item{}, fmt.Errorf("this project has %d cuts, so `cut` has to say which one: %s",
			len(plan.Items), describeCuts(plan))
	}
	for _, item := range plan.Items {
		if item.Cut.ID == wanted || item.Cut.Name == wanted {
			return item, nil
		}
	}
	return export.Item{}, fmt.Errorf("no cut here is named or identified %q — this project has %s", wanted, describeCuts(plan))
}

func describeCuts(plan *export.Plan) string {
	named := make([]string, 0, len(plan.Items))
	for _, item := range plan.Items {
		named = append(named, fmt.Sprintf("%s (id %s, on screen %s)", item.Cut.Name, item.Cut.ID, item.Screen.Name))
	}
	return strings.Join(named, ", ")
}

func validationText(dir string, result validationResult) string {
	where := dir
	if absolute, err := filepath.Abs(dir); err == nil {
		where = absolute
	}

	var text strings.Builder
	fmt.Fprintf(&text, "%s\n", filepath.Join(where, format.ProjectFile))
	for _, problem := range result.Problems {
		fmt.Fprintf(&text, "%s: ", problem.Severity)
		if problem.Line > 0 {
			fmt.Fprintf(&text, "line %d: ", problem.Line)
		}
		fmt.Fprintf(&text, "%s\n", problem.Message)
		if problem.Hint != "" {
			fmt.Fprintf(&text, "       %s\n", problem.Hint)
		}
	}
	if result.OK {
		fmt.Fprintf(&text, "ok — %d screens, %d cuts", result.Screens, result.Cuts)
	}
	return text.String()
}

func decodeArgs(raw json.RawMessage) (callArgs, error) {
	var args callArgs
	if len(raw) == 0 || string(raw) == "null" {
		return args, nil
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, fmt.Errorf("these are not arguments this tool takes: %w", err)
	}
	return args, nil
}

func said(text string) mcp.Result {
	return mcp.Result{Content: []mcp.Content{mcp.Text("%s", text)}}
}

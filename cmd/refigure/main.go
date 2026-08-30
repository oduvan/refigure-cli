// Command refigure turns a Refigure project into its exported images.
//
// It reads refigure.yaml and the screenshots beside it, and writes one image
// per cut. It never writes the project file, and never reads anything the
// desktop app keeps for itself.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	_ "image/jpeg"
	_ "image/png"

	"github.com/oduvan/refigure-cli/internal/export"
	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/lint"
	"github.com/oduvan/refigure-cli/internal/render"
	_ "golang.org/x/image/webp"
)

// Version is set at build time: -ldflags "-X main.Version=v1.2.3".
//
// It must stay a plain string literal: the linker's -X can only replace a
// variable initialised to a constant, so computing it here would silently
// leave every release reporting "dev".
var Version = "dev"

// reportedVersion is what `refigure version` prints.
//
// A binary built with `go install` carries no -ldflags, and reporting "dev" is
// no use to anyone asking whether they are out of date, or filing a bug. Go
// records the module version such a build came from, so fall back to that.
func reportedVersion() string {
	if Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return normalizeVersion(info.Main.Version)
}

// normalizeVersion turns what the module system knows into what a person or a
// script should see. A build from a working tree reports "(devel)", which means
// the same as knowing nothing.
func normalizeVersion(raw string) string {
	if raw == "" || raw == "(devel)" {
		return "dev"
	}
	return raw
}

const usage = `refigure — export a Refigure project to images

A project is a folder holding refigure.yaml and the screenshots it references.
This tool reads that folder and writes one image per cut. It never writes the
project file.

Usage:
  refigure export   [project] [flags]   write one image per cut
  refigure list     [project] [flags]   what would be written, and at what size
  refigure validate [project] [flags]   check refigure.yaml
  refigure schema            [flags]    describe the project file format
  refigure version
  refigure help [command]

The project defaults to the current directory.
Every command takes --help. Every command takes --json.

Exit codes:
  0  success
  1  failure — the message says what
  2  the project file could not be read or is invalid

Writing a project file? Start with:
  refigure schema             the format, explained
  refigure schema --example   a complete file that validates
  refigure schema --json      the same as a JSON Schema
Then check what you wrote:
  refigure validate --json    every problem at once, with line numbers
`

const exportUsage = `refigure export [project] [flags] — write one image per cut

Each cut becomes one file, named after the cut: name + "." + the format's
extension (png, jpg, webp). Two cuts with the same name overwrite each other,
and the command warns when that would happen.

Flags:
  --out DIR        where to write. Overrides export.dest in the project.
                   A relative path resolves against the PROJECT folder, not the
                   working directory.
  --format FORMAT  png, jpeg or webp. Overrides export.format.
  --quality N      1-100, for jpeg and webp. Overrides export.quality.
  --scale N        cap the width at N pixels. Never enlarges: a cut narrower
                   than N is written at its own size.
  --original       ignore any downscale the project asks for.
  --only NAMES     comma-separated cut or screen names.
  --only-id IDS    comma-separated cut ids. Names repeat across screens; ids do
                   not, so a program that means one cut exactly says so here.
  --dry-run        print what would be written, write nothing.
  --json           one JSON document on stdout, described below.
  --progress       one line per image on stderr as it is written:
                   "progress 3/12 connect-token.png". Goes to stderr so it can
                   be combined with --json.
  --font-dir DIR   look here for fonts before the system folders. Repeatable.
                   A missing font is a warning, not a failure, and the text is
                   drawn with a fallback that will not match the editor.

--json prints:
  {
    "dest": "/abs/path",
    "dryRun": false,
    "images": [{"screen":"connect","cut":"token","fileName":"token.png",
                "width":700,"height":380}],
    "written": ["token.png"]
  }

Examples:
  refigure export
  refigure export ./docs/shots --out ./site/img --scale 1400
  refigure export --only-id cut_c3d4 --format webp --quality 80
  refigure export --dry-run --json
`

const listUsage = `refigure list [project] [flags] — what an export would write

Prints one line per cut: screen name, file name, and the size the image would
be, after any downscale the project asks for.

Flags:
  --json   the same information as {"images":[...]}, with the fields described
           in "refigure help export".

Nothing is written, and no folder is created.
`

const validateUsage = `refigure validate [project] [flags] — check refigure.yaml

Flags:
  --json     one document on stdout, whether it passed or not:
             {"ok":true,"screens":1,"cuts":2,"problems":[]}
             Each problem is {"severity","message","line","hint"}.
  --strict   exit 1 when there are warnings as well as errors.

Every problem is reported at once, with the line it is on, so a file can be
fixed in one pass rather than one run per mistake.

Errors are things that make the export fail, or produce an image the file does
not describe: a missing screenshot, a duplicate id, a figure owned by a cut
that does not exist, two cuts on one screen with the same name, a cut with no
area.

Warnings are things that export but are almost certainly mistakes: an unknown
key — usually a typo, and it says which key it looks like — an exclusion of a
figure that is not there, a cut running past the edge of its screenshot, an
owned figure lying outside its own cut, text with no text in it.
`

const schemaUsage = `refigure schema [flags] — describe the project file format

With no flags, prints the format in prose: every key, what it means, and the
two rules that decide which figures a cut contains.

Flags:
  --example   print a complete refigure.yaml that validates
  --json      print the JSON Schema for refigure.yaml

Nothing is read and nothing is written; this describes the format itself.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "export":
		os.Exit(runExport(args))
	case "list":
		os.Exit(runList(args))
	case "validate":
		os.Exit(runValidate(args))
	case "schema":
		os.Exit(runSchema(args))
	case "version", "--version", "-v":
		fmt.Println(reportedVersion())
	case "help", "--help", "-h":
		fmt.Print(helpFor(args))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(1)
	}
}

// helpFor lets `refigure help export` answer the question `refigure export
// --help` answers, so a caller that guesses either way is right.
func helpFor(args []string) string {
	if len(args) == 0 {
		return usage
	}
	switch args[0] {
	case "export":
		return exportUsage
	case "list":
		return listUsage
	case "validate":
		return validateUsage
	case "schema":
		return schemaUsage
	default:
		return usage
	}
}

// wantsHelp answers --help before the flag package can, which would print its
// own terse dump of flag names with no explanation of what they mean.
func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func runExport(args []string) int {
	if wantsHelp(args) {
		fmt.Print(exportUsage)
		return 0
	}
	flags := flag.NewFlagSet("export", flag.ExitOnError)
	out := flags.String("out", "", "output directory")
	outputFormat := flags.String("format", "", "png, jpeg or webp")
	quality := flags.Int("quality", 0, "1-100")
	scale := flags.Int("scale", 0, "max width in pixels")
	original := flags.Bool("original", false, "ignore any downscale")
	only := flags.String("only", "", "comma-separated cut or screen names")
	onlyIDs := flags.String("only-id", "", "comma-separated cut ids")
	dryRun := flags.Bool("dry-run", false, "print what would be written")
	asJSON := flags.Bool("json", false, "machine-readable output")
	progress := flags.Bool("progress", false, "report each image on stderr as it is written")
	var fontDirs stringList
	flags.Var(&fontDirs, "font-dir", "extra font directory")
	dir := parseDir(flags, args)

	project, code := load(dir)
	if project == nil {
		return code
	}

	opts := export.Options{Original: *original, MaxWidth: *scale}
	if *only != "" {
		opts.Only = strings.Split(*only, ",")
	}
	if *onlyIDs != "" {
		opts.OnlyIDs = strings.Split(*onlyIDs, ",")
	}
	if *outputFormat != "" {
		opts.Format = format.ExportFormat(*outputFormat)
	}

	plan, err := export.Build(project, opts)
	if err != nil {
		return fail(err)
	}
	if len(plan.Items) == 0 {
		// Name whichever filter was actually used: a caller passing ids and
		// being told about --only has to go and read the flags to find out
		// which of the two it means.
		switch {
		case *onlyIDs != "":
			return fail(fmt.Errorf("nothing to export — no cut in this project has one of those ids"))
		case *only != "":
			return fail(fmt.Errorf("nothing to export — --only %q matched no cut or screen", *only))
		default:
			return fail(fmt.Errorf("nothing to export — the project has no cuts"))
		}
	}

	dest := *out
	if dest == "" {
		dest = project.Export.Dest
	}
	if dest == "" {
		return fail(fmt.Errorf("no output directory — pass --out, or set `export.dest` in the project"))
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(project.Dir, dest)
	}

	for _, name := range plan.Collisions {
		warn("two cuts are both named %q, so one image will overwrite the other", strings.TrimSuffix(name, filepath.Ext(name)))
	}

	if *dryRun {
		return report(*asJSON, dest, plan, nil, true)
	}

	if err := export.EnsureDir(dest); err != nil {
		return fail(err)
	}

	outputFmt := project.Export.Format
	if opts.Format != "" {
		outputFmt = opts.Format
	}
	q := project.Export.Quality
	if *quality > 0 {
		q = *quality
	}

	missingFonts := map[string]bool{}
	renderOpts := render.Options{
		FontDirs: fontDirs,
		OnMissingFont: func(family string) {
			if !missingFonts[family] {
				missingFonts[family] = true
				warn("font %q was not found, so text is drawn with a fallback and will not match the editor", family)
			}
		},
	}

	screenshots := map[string]image.Image{}
	var written []string

	for _, item := range plan.Items {
		screenshot, ok := screenshots[item.Screen.File]
		if !ok {
			loaded, err := loadImage(filepath.Join(project.Dir, item.Screen.File))
			if err != nil {
				return fail(fmt.Errorf("screen %q: %w", item.Screen.Name, err))
			}
			screenshots[item.Screen.File] = loaded
			screenshot = loaded
		}

		screen := item.Screen
		img, err := render.Cut(screenshot, item.Rect, item.Figures, func(f *format.Figure) format.ResolvedStyle {
			return project.StyleFor(screen, f)
		}, renderOpts)
		if err != nil {
			return fail(fmt.Errorf("cut %q: %w", item.Cut.Name, err))
		}

		if item.Scale != 1 {
			img = render.Resize(img, item.Width, item.Height)
		}
		if err := export.Encode(img, filepath.Join(dest, item.FileName), outputFmt, q); err != nil {
			return fail(fmt.Errorf("cut %q: %w", item.Cut.Name, err))
		}
		written = append(written, item.FileName)

		// Progress goes to stderr so stdout stays exactly what it was — human
		// text, or one JSON document. A caller wanting a progress bar reads
		// these lines; anyone else never sees them.
		if *progress {
			fmt.Fprintf(os.Stderr, "progress %d/%d %s\n", len(written), len(plan.Items), item.FileName)
		}
	}

	return report(*asJSON, dest, plan, written, false)
}

func runList(args []string) int {
	if wantsHelp(args) {
		fmt.Print(listUsage)
		return 0
	}
	flags := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := flags.Bool("json", false, "machine-readable output")
	dir := parseDir(flags, args)

	project, code := load(dir)
	if project == nil {
		return code
	}
	plan, err := export.Build(project, export.Options{})
	if err != nil {
		return fail(err)
	}

	if *asJSON {
		return emit(planJSON("", plan, nil))
	}
	for _, item := range plan.Items {
		fmt.Printf("%-24s %-24s %dx%d\n", item.Screen.Name, item.FileName, item.Width, item.Height)
	}
	return 0
}

func runValidate(args []string) int {
	if wantsHelp(args) {
		fmt.Print(validateUsage)
		return 0
	}
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	asJSON := flags.Bool("json", false, "machine-readable output")
	strict := flags.Bool("strict", false, "treat warnings as failures")
	dir := parseDir(flags, args)

	project, err := format.Load(dir)
	if err != nil {
		// The file could not be read at all, so there is nothing to check
		// beyond saying where it went wrong.
		problem := lint.Problem{Severity: lint.SeverityError, Message: err.Error()}
		var formatErr *format.Error
		if errors.As(err, &formatErr) {
			problem.Message, problem.Line = formatErr.Message, formatErr.Line
		}
		return reportValidation(*asJSON, 0, 0, []lint.Problem{problem})
	}

	cuts := 0
	for _, screen := range project.Screens {
		cuts += len(screen.Cuts)
	}

	// The raw bytes again, for line numbers and for the keys nobody read.
	var problems []lint.Problem
	if data, readErr := os.ReadFile(filepath.Join(dir, format.ProjectFile)); readErr == nil {
		problems = lint.Check(dir, data, project)
	}

	code := reportValidation(*asJSON, len(project.Screens), cuts, problems)
	if code == 0 && *strict && len(problems) > 0 {
		return 1
	}
	return code
}

// reportValidation prints every problem at once. A caller fixing a file wants
// the whole list, not the first thing that went wrong followed by another run.
func reportValidation(asJSON bool, screens, cuts int, problems []lint.Problem) int {
	failed := false
	for _, problem := range problems {
		if problem.Severity == lint.SeverityError {
			failed = true
		}
	}

	if asJSON {
		emit(validationResult{OK: !failed, Screens: screens, Cuts: cuts, Problems: problems})
		if failed {
			return 2
		}
		return 0
	}

	for _, problem := range problems {
		where := ""
		if problem.Line > 0 {
			where = fmt.Sprintf("line %d: ", problem.Line)
		}
		fmt.Fprintf(os.Stderr, "%s: %s%s\n", problem.Severity, where, problem.Message)
		if problem.Hint != "" {
			fmt.Fprintf(os.Stderr, "       %s\n", problem.Hint)
		}
	}

	if failed {
		return 2
	}
	if len(problems) > 0 {
		fmt.Printf("ok — %d screens, %d cuts, %d warning%s\n", screens, cuts, len(problems), plural(len(problems)))
		return 0
	}
	fmt.Printf("ok — %d screens, %d cuts\n", screens, cuts)
	return 0
}

type validationResult struct {
	OK       bool           `json:"ok"`
	Screens  int            `json:"screens"`
	Cuts     int            `json:"cuts"`
	Problems []lint.Problem `json:"problems"`
}

func runSchema(args []string) int {
	if wantsHelp(args) {
		fmt.Print(schemaUsage)
		return 0
	}
	flags := flag.NewFlagSet("schema", flag.ExitOnError)
	asJSON := flags.Bool("json", false, "print the JSON Schema")
	example := flags.Bool("example", false, "print a complete project file")
	_ = flags.Parse(args)

	switch {
	case *asJSON:
		fmt.Print(format.SchemaJSON)
	case *example:
		fmt.Print(format.SchemaExample)
	default:
		fmt.Print(format.SchemaReference)
	}
	return 0
}

func parseDir(flags *flag.FlagSet, args []string) string {
	// The project path may come before or after the flags.
	var positional []string
	var rest []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || len(rest) > 0 {
			rest = append(rest, arg)
		} else {
			positional = append(positional, arg)
		}
	}
	_ = flags.Parse(rest)
	positional = append(positional, flags.Args()...)
	if len(positional) > 0 {
		return positional[0]
	}
	return "."
}

func load(dir string) (*format.Project, int) {
	project, err := format.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "refigure: %s\n", err)
		return nil, 2
	}
	return project, 0
}

func loadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("%s is missing", filepath.Base(path))
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %w", filepath.Base(path), err)
	}
	return img, nil
}

type output struct {
	Dest    string       `json:"dest"`
	DryRun  bool         `json:"dryRun,omitempty"`
	Images  []outputItem `json:"images"`
	Written []string     `json:"written,omitempty"`
}

type outputItem struct {
	Screen   string `json:"screen"`
	Cut      string `json:"cut"`
	FileName string `json:"fileName"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

func planJSON(dest string, plan *export.Plan, written []string) output {
	result := output{Dest: dest, Written: written}
	for _, item := range plan.Items {
		result.Images = append(result.Images, outputItem{
			Screen:   item.Screen.Name,
			Cut:      item.Cut.Name,
			FileName: item.FileName,
			Width:    item.Width,
			Height:   item.Height,
		})
	}
	return result
}

func report(asJSON bool, dest string, plan *export.Plan, written []string, dryRun bool) int {
	if asJSON {
		result := planJSON(dest, plan, written)
		result.DryRun = dryRun
		return emit(result)
	}
	if dryRun {
		for _, item := range plan.Items {
			fmt.Printf("would write %s (%dx%d)\n", filepath.Join(dest, item.FileName), item.Width, item.Height)
		}
		return 0
	}
	sort.Strings(written)
	fmt.Printf("%d image%s written to %s\n", len(written), plural(len(written)), dest)
	return 0
}

func emit(value any) int {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fail(err)
	}
	fmt.Println(string(encoded))
	return 0
}

func warn(message string, args ...any) {
	fmt.Fprintf(os.Stderr, "refigure: warning: "+message+"\n", args...)
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "refigure: %s\n", err)
	return 1
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

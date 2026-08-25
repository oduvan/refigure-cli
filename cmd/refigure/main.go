// Command refigure turns a Refigure project into its exported images.
//
// It reads refigure.yaml and the screenshots beside it, and writes one image
// per cut. It never writes the project file, and never reads anything the
// desktop app keeps for itself.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "image/jpeg"
	_ "image/png"

	"github.com/oduvan/refigure-cli/internal/export"
	"github.com/oduvan/refigure-cli/internal/format"
	"github.com/oduvan/refigure-cli/internal/render"
	_ "golang.org/x/image/webp"
)

// Version is set at build time: -ldflags "-X main.Version=v1.2.3"
var Version = "dev"

const usage = `refigure — export a Refigure project to images

Usage:
  refigure export [project] [flags]   write one image per cut
  refigure list   [project]           show the cuts and the file names they produce
  refigure validate [project]         check refigure.yaml, print nothing if it is fine
  refigure version

The project defaults to the current directory.

Export flags:
  --out DIR        where to write; overrides the project's own setting
  --format FORMAT  png, jpeg or webp
  --quality N      1-100, for jpeg and webp
  --scale N        cap the width at N pixels; never enlarges
  --original       ignore any downscale the project asks for
  --only NAMES     comma-separated cut or screen names
  --only-id IDS    comma-separated cut ids, for tools that need to be exact
  --dry-run        print what would be written, write nothing
  --json           machine-readable output, for tools
  --progress       report each image on stderr as it is written
  --font-dir DIR   look here for fonts first; repeatable

Exit codes:
  0  success
  1  failure — the message says what
  2  the project file could not be read or is invalid
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
	case "version", "--version", "-v":
		fmt.Println(Version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		os.Exit(1)
	}
}

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func runExport(args []string) int {
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
		return fail(fmt.Errorf("nothing to export — the project has no cuts, or --only matched none"))
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
	flags := flag.NewFlagSet("validate", flag.ExitOnError)
	dir := parseDir(flags, args)
	project, code := load(dir)
	if project == nil {
		return code
	}
	cuts := 0
	for _, screen := range project.Screens {
		cuts += len(screen.Cuts)
	}
	fmt.Printf("ok — %d screens, %d cuts\n", len(project.Screens), cuts)
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

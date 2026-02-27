package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	opts := Options{}
	var protoPaths multiFlag

	flag.BoolVar(&opts.Recursive, "r", false, "Recursively process all .proto files in directories")
	flag.BoolVar(&opts.Recursive, "recursive", false, "Recursively process all .proto files in directories")
	flag.BoolVar(&opts.Write, "w", false, "Write changes in-place")
	flag.BoolVar(&opts.Write, "write", false, "Write changes in-place")
	flag.BoolVar(&opts.Check, "c", false, "Exit non-zero if file would change (for CI)")
	flag.BoolVar(&opts.Check, "check", false, "Exit non-zero if file would change (for CI)")
	flag.BoolVar(&opts.Diff, "d", false, "Print unified diff of changes")
	flag.BoolVar(&opts.Diff, "diff", false, "Print unified diff of changes")
	flag.BoolVar(&opts.Verify, "verify", false, "Run protoc descriptor verification after sorting")
	flag.StringVar(&opts.ProtocPath, "protoc", "", "Path to protoc binary")
	flag.Var(&protoPaths, "proto-path", "Additional proto include paths (repeatable)")
	flag.StringVar(&opts.SharedOrder, "shared-order", "alpha", "Ordering for core types: alpha or dependency")
	flag.StringVar(&opts.SortRPCs, "sort-rpcs", "", "Sort RPCs within services: alpha or grouped")
	flag.BoolVar(&opts.PreserveDividers, "preserve-dividers", false, "Keep section divider comments")
	flag.BoolVar(&opts.StripCommented, "strip-commented-code", false, "Remove commented-out protobuf declarations")
	flag.BoolVar(&opts.DryRun, "dry-run", false, "Report what would change without writing")
	flag.BoolVar(&opts.Verbose, "v", false, "Print reference counts and classification")
	flag.BoolVar(&opts.Verbose, "verbose", false, "Print reference counts and classification")
	flag.BoolVar(&opts.Quiet, "q", false, "Suppress warnings")
	flag.BoolVar(&opts.Quiet, "quiet", false, "Suppress warnings")
	flag.BoolVar(&opts.Annotate, "annotate", false, "Add classification annotations to comments")
	flag.BoolVar(&opts.SectionHeaders, "section-headers", false, "Insert section header comments")
	flag.StringVar(&opts.ConfigFile, "config", "", "Path to .protosort.toml config file")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: protosort [OPTIONS] <FILE|DIR>...\n\n")
		fmt.Fprintf(os.Stderr, "Reorder top-level declarations in proto3 .proto files.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()
	opts.ProtoPaths = []string(protoPaths)

	// Track which flags were explicitly set on the command line
	setFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	// Load .protosort.toml config if available
	configPath := opts.ConfigFile
	if configPath == "" {
		configPath = findConfigFile()
	}
	if configPath != "" {
		cfg, err := LoadConfig(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to load config %s: %v\n", configPath, err)
		} else {
			MergeConfig(&opts, cfg, setFlags)
		}
	}

	if opts.SharedOrder != "alpha" && opts.SharedOrder != "dependency" {
		fmt.Fprintf(os.Stderr, "error: --shared-order must be \"alpha\" or \"dependency\", got %q\n", opts.SharedOrder)
		os.Exit(4)
	}

	if opts.SortRPCs != "" && opts.SortRPCs != "alpha" && opts.SortRPCs != "grouped" {
		fmt.Fprintf(os.Stderr, "error: --sort-rpcs must be \"alpha\" or \"grouped\", got %q\n", opts.SortRPCs)
		os.Exit(4)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(4)
	}

	// Collect all .proto files
	files, err := collectFiles(args, opts.Recursive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(4)
	}

	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "error: no .proto files found\n")
		os.Exit(4)
	}

	exitCode := 0
	for _, file := range files {
		code := processFile(file, opts)
		if code > exitCode {
			exitCode = code
		}
	}

	os.Exit(exitCode)
}

func processFile(file string, opts Options) int {
	info, err := os.Stat(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", file, err)
		return 4
	}
	fileMode := info.Mode()

	content, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", file, err)
		return 4
	}

	original := string(content)

	sorted, warnings, err := Sort(original, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s: %v\n", file, err)
		var proto2Err *Proto2Error
		var parseErr *ParseError
		if errors.As(err, &proto2Err) || errors.As(err, &parseErr) {
			return 3
		}
		return 4
	}

	// Print warnings
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "%s: %s\n", file, w)
	}

	// Verbose output
	if opts.Verbose {
		blocks, _ := ScanFile(original)
		fmt.Fprint(os.Stderr, VerboseReport(blocks))
	}

	// No changes needed
	if original == sorted {
		if !opts.Quiet {
			if opts.Check || opts.DryRun {
				fmt.Fprintf(os.Stderr, "%s: no changes needed\n", file)
			}
		}
		return 0
	}

	// Verify (if requested)
	if opts.Verify && !opts.DryRun {
		if err := Verify(original, sorted, opts); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: verification failed: %v\n", file, err)
			return 2
		}
	}

	// Check mode
	if opts.Check {
		fmt.Fprintf(os.Stderr, "%s: would change\n", file)
		if opts.Diff {
			fmt.Print(DiffStrings(original, sorted, file+" (original)", file+" (sorted)"))
		}
		return 1
	}

	// Dry run
	if opts.DryRun {
		fmt.Fprintf(os.Stderr, "%s: would change\n", file)
		if opts.Diff {
			fmt.Print(DiffStrings(original, sorted, file+" (original)", file+" (sorted)"))
		}
		return 0
	}

	// Write mode
	if opts.Write {
		if err := os.WriteFile(file, []byte(sorted), fileMode.Perm()); err != nil {
			fmt.Fprintf(os.Stderr, "error writing %s: %v\n", file, err)
			return 4
		}
		if opts.Diff {
			fmt.Print(DiffStrings(original, sorted, file+" (original)", file+" (sorted)"))
		}
		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "%s: sorted\n", file)
		}
		return 0
	}

	// Diff mode (without write)
	if opts.Diff {
		fmt.Print(DiffStrings(original, sorted, file+" (original)", file+" (sorted)"))
		return 0
	}

	// Default: print to stdout
	fmt.Print(sorted)
	return 0
}

func collectFiles(args []string, recursive bool) ([]string, error) {
	var files []string

	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("cannot access %s: %w", arg, err)
		}

		if !info.IsDir() {
			if !strings.HasSuffix(arg, ".proto") {
				return nil, fmt.Errorf("%s is not a .proto file", arg)
			}
			files = append(files, arg)
			continue
		}

		if !recursive {
			// Non-recursive: only immediate .proto files
			entries, err := os.ReadDir(arg)
			if err != nil {
				return nil, fmt.Errorf("reading directory %s: %w", arg, err)
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".proto") {
					files = append(files, filepath.Join(arg, entry.Name()))
				}
			}
		} else {
			// Recursive walk
			err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(d.Name(), ".proto") {
					files = append(files, path)
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("walking directory %s: %w", arg, err)
			}
		}
	}

	return files, nil
}

// multiFlag implements flag.Value for repeatable string flags.
type multiFlag []string

func (f *multiFlag) String() string {
	return strings.Join(*f, ", ")
}

func (f *multiFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

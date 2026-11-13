package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const profileCLIHelp = productName + " " + productVersion + `

Run without arguments to open the desktop app.

  export --output FILE.mcpd [--workspace DIR] [--overwrite]
  import --input FILE.mcpd [--workspace DIR] [--dry-run]
  --version

Import also accepts a legacy .json profile. Use --workspace for workspace
registrations. Without it, export reads user configurations only.
Success prints one JSON object to stdout. Errors go to stderr.
`

func localManager() (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Manager{}, fmt.Errorf("find user home: %w", err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return Manager{}, fmt.Errorf("find user config directory: %w", err)
	}
	return Manager{Home: home, StateDir: filepath.Join(configDir, stateDirName)}, nil
}

type importCLIResult struct {
	File     string `json:"file"`
	DryRun   bool   `json:"dry_run"`
	Added    int    `json:"added"`
	Paused   int    `json:"paused"`
	Kept     int    `json:"kept"`
	Settings int    `json:"settings"`
}

func runProfileCLI(args []string, stdout, stderr io.Writer, manager func() (Manager, error)) int {
	if len(args) == 0 || (len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h")) {
		fmt.Fprint(stdout, profileCLIHelp)
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, productName, productVersion)
		return 0
	}

	command := args[0]
	fail := func(code int, err error) int {
		fmt.Fprintf(stderr, "%s: %v\n", productName, err)
		return code
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var file, workspace string
	var overwrite, dryRun bool
	flags.StringVar(&workspace, "workspace", "", "workspace folder")
	switch command {
	case "export":
		flags.StringVar(&file, "output", "", "output .mcpd file")
		flags.BoolVar(&overwrite, "overwrite", false, "replace an existing regular output file")
	case "import":
		flags.StringVar(&file, "input", "", "input .mcpd or legacy .json profile")
		flags.BoolVar(&dryRun, "dry-run", false, "check what would change without writing")
	default:
		return fail(2, fmt.Errorf("unknown command %q; use --help", command))
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, profileCLIHelp)
			return 0
		}
		return fail(2, err)
	}
	if flags.NArg() != 0 {
		return fail(2, errors.New("unexpected positional arguments; use --help"))
	}
	if strings.TrimSpace(file) == "" {
		name := "--input"
		if command == "export" {
			name = "--output"
		}
		return fail(2, fmt.Errorf("%s is required for %s", name, command))
	}
	if command == "export" && !strings.EqualFold(filepath.Ext(file), profileExtension) {
		return fail(2, errors.New("the output file must use the .mcpd extension"))
	}
	file, err := filepath.Abs(file)
	if err != nil {
		return fail(1, err)
	}
	m, err := manager()
	if err != nil {
		return fail(1, err)
	}
	// An unattended run uses only the explicitly supplied workspace.
	m.Project = ""
	if workspace != "" {
		m.Project, err = cliWorkspace(workspace)
		if err != nil {
			return fail(1, err)
		}
	}

	var result any
	if command == "export" {
		p, err := m.exportProfile()
		if err != nil {
			return fail(1, err)
		}
		if err := writeProfile(file, p, overwrite); err != nil {
			if errors.Is(err, fs.ErrExist) {
				err = fmt.Errorf("%w; use --overwrite to replace an existing output", err)
			}
			return fail(1, err)
		}
		result = struct {
			File         string `json:"file"`
			Destinations int    `json:"destinations"`
		}{file, len(p.Destinations)}
	} else {
		p, err := openCLIProfile(file)
		if err != nil {
			return fail(1, err)
		}
		plan, err := m.planProfile(p)
		if err != nil {
			return fail(1, err)
		}
		var report ProfileReport
		if dryRun {
			report, _, err = m.prepareProfileImport(plan)
		} else {
			report, err = m.importProfile(plan)
		}
		if err != nil {
			return fail(1, err)
		}
		result = importCLIResult{
			File: file, DryRun: dryRun, Added: report.Added, Paused: report.Paused,
			Kept: report.Kept, Settings: report.Settings,
		}
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return fail(1, fmt.Errorf("write command result: %w", err))
	}
	return 0
}

func cliWorkspace(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	project, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	info, err := os.Stat(project)
	if err != nil {
		return "", fmt.Errorf("read workspace: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("the workspace path must be a directory")
	}
	return project, nil
}

func openCLIProfile(path string) (Profile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Profile{}, fmt.Errorf("open profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Profile{}, errors.New("the input profile must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return Profile{}, fmt.Errorf("open profile: %w", err)
	}
	defer file.Close()
	return readProfileFile(path, file)
}

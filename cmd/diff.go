package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/ai"
	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/renderer"
	"github.com/dormstern/segspec/internal/walker"
)

var diffExitCode bool

// errChangesDetected is returned when --exit-code is set and changes are found.
// The root command maps this to exit code 1.
var errChangesDetected = fmt.Errorf("changes detected")

const maxBaselineSize = 10 * 1024 * 1024 // 10MB

var diffCmd = &cobra.Command{
	Use:   "diff <baseline.json> <path>",
	Short: "Compare network dependencies against a baseline",
	Long: `Diff compares a baseline JSON file (from --format json) against a fresh
analysis of a directory or GitHub URL. Shows added, removed, and unchanged
dependencies.

Use --exit-code to exit with code 1 if changes are detected (for CI/CD).`,
	Args: cobra.ExactArgs(2),
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().BoolVar(&diffExitCode, "exit-code", false, "Exit with code 1 if changes detected")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	baselineFile := args[0]
	path := args[1]

	// Check baseline file size before reading.
	fi, err := os.Stat(baselineFile)
	if err != nil {
		return fmt.Errorf("cannot read baseline file: %w", err)
	}
	if fi.Size() > maxBaselineSize {
		return fmt.Errorf("baseline file too large (%d bytes, max %d)", fi.Size(), maxBaselineSize)
	}

	data, err := os.ReadFile(baselineFile)
	if err != nil {
		return fmt.Errorf("cannot read baseline file: %w", err)
	}
	var baseline model.DependencySet
	if err := json.Unmarshal(data, &baseline); err != nil {
		return fmt.Errorf("cannot parse baseline JSON: %w", err)
	}

	// Resolve the target path (GitHub URL or local directory).
	repoName := ""
	if isGitHubURL(path) {
		u := normalizeGitHubURL(path)
		repoName = repoNameFromURL(u)
		fmt.Fprintf(os.Stderr, "Cloning %s...\n", u)
		cloneDir, err := cloneRepo(u)
		if err != nil {
			return err
		}
		defer os.RemoveAll(cloneDir)
		path = cloneDir
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	// Analyze the current directory.
	registry := parser.DefaultRegistry()
	walkOpts := walker.WalkOptions{HelmValuesFile: helmValuesFile}
	current, warnings, err := walker.Walk(path, registry, walkOpts)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}
	if repoName != "" {
		current.RenameSource(current.ServiceName, repoName)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d file(s) could not be parsed\n", len(warnings))
	}

	if aiProvider != "" {
		aiDeps, aiErr := ai.Analyze(path, current.Dependencies(), aiProvider)
		if aiErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: AI analysis skipped: %v\n", aiErr)
		} else {
			for _, dep := range aiDeps {
				current.Add(dep)
			}
		}
	}

	// Compute and render the diff.
	diff := model.DiffSets(&baseline, current)

	out := cmd.OutOrStdout()
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	fmt.Fprint(out, renderer.Diff(diff))

	if diffExitCode && (len(diff.Added) > 0 || len(diff.Removed) > 0) {
		cmd.SilenceErrors = true
		return errChangesDetected
	}

	return nil
}

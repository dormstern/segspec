package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
	"github.com/dormstern/segspec/internal/walker"
)

// SnapshotMetadata captures provenance for a baseline file. Embedded in
// snapshot files under the top-level "metadata" key so future `diff` runs can
// detect the producing segspec version, and so users have an audit trail of
// when and where the baseline came from.
type SnapshotMetadata struct {
	CreatedUTC     string `json:"created_utc"`
	SegspecVersion string `json:"segspec_version"`
	GitCommit      string `json:"git_commit"`
	GitDirty       bool   `json:"git_dirty"`
	InputPath      string `json:"input_path"`
}

// snapshotFile is the wire format for `segspec snapshot` output. Backward
// compatible: legacy `--format json` output (a bare DependencySet) is still
// accepted by `diff` because we attempt to unmarshal both shapes.
type snapshotFile struct {
	Metadata     SnapshotMetadata          `json:"metadata"`
	Service      string                    `json:"service"`
	Generated    string                    `json:"generated"`
	Version      string                    `json:"version"`
	Dependencies []model.NetworkDependency `json:"dependencies"`
}

var snapshotCmd = &cobra.Command{
	Use:   "snapshot <path>",
	Short: "Capture a dependency baseline with provenance metadata",
	Long: `Snapshot scans a directory and writes a baseline JSON file containing
the discovered dependency set plus a metadata block (timestamp, git commit,
segspec version, input path).

The output is a drop-in replacement for the legacy '--format json' baseline
used by 'segspec diff'; the diff command transparently accepts both shapes.

Use -o to write to a file (default: stdout).`,
	Args: cobra.ExactArgs(1),
	RunE: runSnapshot,
}

func init() {
	rootCmd.AddCommand(snapshotCmd)
}

func runSnapshot(cmd *cobra.Command, args []string) error {
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	registry := parser.DefaultRegistry()
	walkOpts := walker.WalkOptions{HelmValuesFile: helmValuesFile}
	ds, warnings, err := walker.Walk(path, registry, walkOpts)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}
	if len(warnings) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d file(s) could not be parsed\n", len(warnings))
	}

	absPath, absErr := filepath.Abs(path)
	if absErr != nil {
		absPath = path
	}

	commit, dirty := gitProvenance(path)
	meta := SnapshotMetadata{
		CreatedUTC:     time.Now().UTC().Format(time.RFC3339),
		SegspecVersion: Version,
		GitCommit:      commit,
		GitDirty:       dirty,
		InputPath:      absPath,
	}

	snap := snapshotFile{
		Metadata:     meta,
		Service:      ds.ServiceName,
		Generated:    time.Now().UTC().Format("2006-01-02"),
		Version:      Version,
		Dependencies: ds.Dependencies(),
	}

	out := cmd.OutOrStdout()
	if outputFile != "" {
		f, err := os.Create(outputFile)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer f.Close()
		out = f
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal snapshot: %w", err)
	}
	fmt.Fprintln(out, string(data))
	return nil
}

// gitProvenance returns ("HEAD-sha", dirty?) for the git repo containing path,
// or ("unknown", false) if path is not in a git repo or git is unavailable.
// Best-effort — we never fail the snapshot just because git data couldn't be
// gathered.
func gitProvenance(path string) (string, bool) {
	revCmd := exec.Command("git", "rev-parse", "HEAD")
	revCmd.Dir = path
	out, err := revCmd.Output()
	if err != nil {
		return "unknown", false
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "unknown", false
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = path
	statusOut, statusErr := statusCmd.Output()
	dirty := false
	if statusErr == nil && len(strings.TrimSpace(string(statusOut))) > 0 {
		dirty = true
	}
	return commit, dirty
}

// readBaseline accepts either the new snapshot format (with a metadata block)
// or the legacy bare DependencySet format and returns the dependency set plus
// metadata when present. Backward-compat is non-negotiable: a legacy baseline
// must continue to work silently.
func readBaseline(data []byte) (*model.DependencySet, *SnapshotMetadata, error) {
	// Probe for a metadata block. We can't rely on Unmarshal-into-snapshotFile
	// alone because Go's encoding/json silently drops unknown fields, so a
	// legacy file would still populate metadata as a zero value and we'd
	// emit a bogus version-mismatch warning.
	var probe struct {
		Metadata *json.RawMessage `json:"metadata"`
	}
	_ = json.Unmarshal(data, &probe)

	ds := &model.DependencySet{}
	if err := json.Unmarshal(data, ds); err != nil {
		return nil, nil, err
	}

	if probe.Metadata == nil {
		return ds, nil, nil
	}

	var meta SnapshotMetadata
	if err := json.Unmarshal(*probe.Metadata, &meta); err != nil {
		// Malformed metadata — treat as legacy. Don't punish users for a
		// bad metadata block when the dependencies parsed fine.
		return ds, nil, nil
	}
	return ds, &meta, nil
}

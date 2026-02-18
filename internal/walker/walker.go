package walker

import (
	"os"
	"path/filepath"

	"github.com/dormstern/segspec/internal/model"
	"github.com/dormstern/segspec/internal/parser"
)

// skippedDirs are directories we never descend into.
var skippedDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	".git":         true,
	".svn":         true,
	"__pycache__":  true,
}

// WalkWarning represents a non-fatal error encountered while walking.
// Typically this is a per-file parse failure.
type WalkWarning struct {
	File string
	Err  error
}

// detectHelmCharts finds directories containing Chart.yaml under root.
func detectHelmCharts(root string) []string {
	var charts []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "Chart.yaml" {
			charts = append(charts, filepath.Dir(path))
		}
		return nil
	})
	return charts
}

// WalkOptions configures optional behavior for Walk.
type WalkOptions struct {
	HelmValuesFile string // Helm values file to use when rendering charts (optional)
}

// Walk recursively scans root for files matching registered parsers,
// collects all discovered network dependencies, and returns them as a DependencySet.
// Per-file parse failures are returned as warnings (not fatal errors).
// The error return is reserved for fatal errors such as inability to walk the directory.
func Walk(root string, registry *parser.Registry, opts ...WalkOptions) (*model.DependencySet, []WalkWarning, error) {
	var options WalkOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	serviceName := filepath.Base(root)
	ds := model.NewDependencySet(serviceName)
	var warnings []WalkWarning

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if d.IsDir() {
			if skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		parsers := registry.Match(path)
		for _, fn := range parsers {
			deps, parseErr := fn(path)
			if parseErr != nil {
				relPath, relErr := filepath.Rel(root, path)
				if relErr != nil {
					relPath = path
				}
				warnings = append(warnings, WalkWarning{File: relPath, Err: parseErr})
				continue
			}
			for i := range deps {
				if deps[i].Source == "" {
					deps[i].Source = serviceName
				}
				ds.Add(deps[i])
			}
		}
		return nil
	})

	// After normal file walk, detect and process Helm charts
	charts := detectHelmCharts(root)
	for _, chartDir := range charts {
		rendered, renderErr := renderHelmTemplate(chartDir, options.HelmValuesFile)
		if renderErr != nil {
			relPath, relErr := filepath.Rel(root, chartDir)
			if relErr != nil {
				relPath = chartDir
			}
			warnings = append(warnings, WalkWarning{File: relPath + "/Chart.yaml", Err: renderErr})
			continue
		}
		relPath, relErr := filepath.Rel(root, chartDir)
		if relErr != nil {
			relPath = chartDir
		}
		sourceLabel := relPath + "/Chart.yaml (helm template)"
		deps, parseErr := parser.ParseK8sContent(rendered, sourceLabel)
		if parseErr != nil {
			warnings = append(warnings, WalkWarning{File: relPath, Err: parseErr})
			continue
		}
		for i := range deps {
			if deps[i].Source == "" {
				deps[i].Source = serviceName
			}
			ds.Add(deps[i])
		}
	}

	return ds, warnings, err
}

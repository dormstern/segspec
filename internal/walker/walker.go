package walker

import (
	"os"
	"path/filepath"

	"github.com/dormorgenstern/segspec/internal/model"
	"github.com/dormorgenstern/segspec/internal/parser"
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

// Walk recursively scans root for files matching registered parsers,
// collects all discovered network dependencies, and returns them as a DependencySet.
func Walk(root string, registry *parser.Registry) (*model.DependencySet, error) {
	serviceName := filepath.Base(root)
	ds := model.NewDependencySet(serviceName)

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
				continue // skip unparseable files
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

	return ds, err
}

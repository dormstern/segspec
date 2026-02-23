package model

import "sort"

// DependencyDiff represents the difference between two dependency sets.
type DependencyDiff struct {
	Added     []NetworkDependency
	Removed   []NetworkDependency
	Unchanged []NetworkDependency
}

// DiffSets compares baseline and current dependency sets.
// Dependencies are matched by Key() (source->target:port/protocol).
// Results are sorted by Key() for deterministic output.
func DiffSets(baseline, current *DependencySet) DependencyDiff {
	if baseline == nil {
		baseline = NewDependencySet("")
	}
	if current == nil {
		current = NewDependencySet("")
	}

	baselineMap := make(map[string]NetworkDependency)
	for _, dep := range baseline.Dependencies() {
		baselineMap[dep.Key()] = dep
	}

	currentMap := make(map[string]NetworkDependency)
	for _, dep := range current.Dependencies() {
		currentMap[dep.Key()] = dep
	}

	var diff DependencyDiff

	for key, dep := range currentMap {
		if _, exists := baselineMap[key]; !exists {
			diff.Added = append(diff.Added, dep)
		} else {
			diff.Unchanged = append(diff.Unchanged, dep)
		}
	}

	for key, dep := range baselineMap {
		if _, exists := currentMap[key]; !exists {
			diff.Removed = append(diff.Removed, dep)
		}
	}

	sortDeps := func(deps []NetworkDependency) {
		sort.Slice(deps, func(i, j int) bool {
			return deps[i].Key() < deps[j].Key()
		})
	}
	sortDeps(diff.Added)
	sortDeps(diff.Removed)
	sortDeps(diff.Unchanged)

	return diff
}

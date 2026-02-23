package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Confidence indicates how certain we are about a discovered dependency.
type Confidence string

const (
	High   Confidence = "high"   // Explicit URL/host:port in config
	Medium Confidence = "medium" // Parsed from env var or partial config
	Low    Confidence = "low"    // Inferred from build dependencies
)

// NetworkDependency represents a discovered network connection requirement.
type NetworkDependency struct {
	Source       string     `json:"source"`
	Target       string     `json:"target"`
	Port         int        `json:"port"`
	Protocol     string     `json:"protocol"`
	Description  string     `json:"description"`
	Confidence   Confidence `json:"confidence"`
	SourceFile   string     `json:"source_file"`
	EvidenceLine string     `json:"evidence_line,omitempty"`
	ServiceType  string     `json:"service_type,omitempty"`
}

// Key returns a unique identifier for deduplication.
func (d NetworkDependency) Key() string {
	return fmt.Sprintf("%s->%s:%d/%s", d.Source, d.Target, d.Port, d.Protocol)
}

// DependencySet collects network dependencies for a service with deduplication.
type DependencySet struct {
	ServiceName string
	deps        []NetworkDependency
	seen        map[string]bool
}

// NewDependencySet creates an empty set for the named service.
func NewDependencySet(name string) *DependencySet {
	return &DependencySet{
		ServiceName: name,
		deps:        make([]NetworkDependency, 0),
		seen:        make(map[string]bool),
	}
}

// Add inserts a dependency, skipping duplicates by Key().
func (ds *DependencySet) Add(dep NetworkDependency) {
	key := dep.Key()
	if ds.seen[key] {
		return
	}
	ds.seen[key] = true
	ds.deps = append(ds.deps, dep)
}

// Dependencies returns all dependencies sorted by Key() for deterministic output.
func (ds *DependencySet) Dependencies() []NetworkDependency {
	sorted := make([]NetworkDependency, len(ds.deps))
	copy(sorted, ds.deps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Key() < sorted[j].Key()
	})
	return sorted
}

// Merge adds all dependencies from another set into this one.
func (ds *DependencySet) Merge(other *DependencySet) {
	for _, dep := range other.deps {
		ds.Add(dep)
	}
}

// Sources returns a sorted, deduplicated list of all source service names.
func (ds *DependencySet) Sources() []string {
	seen := make(map[string]bool)
	for _, dep := range ds.deps {
		if dep.Source != "" {
			seen[dep.Source] = true
		}
	}
	sources := make([]string, 0, len(seen))
	for s := range seen {
		sources = append(sources, s)
	}
	sort.Strings(sources)
	return sources
}

// IngressFor returns all dependencies whose Target matches the given service name,
// sorted by Key(). These represent inbound connections to that service.
func (ds *DependencySet) IngressFor(service string) []NetworkDependency {
	var result []NetworkDependency
	for _, dep := range ds.deps {
		if dep.Target == service {
			result = append(result, dep)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}

// EgressFor returns all dependencies whose Source matches the given service name,
// sorted by Key(). These represent outbound connections from that service.
func (ds *DependencySet) EgressFor(service string) []NetworkDependency {
	var result []NetworkDependency
	for _, dep := range ds.deps {
		if dep.Source == service {
			result = append(result, dep)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key() < result[j].Key()
	})
	return result
}

// Len returns the number of unique dependencies.
func (ds *DependencySet) Len() int {
	return len(ds.deps)
}

// RenameSource replaces all occurrences of oldName in dep Source fields
// (and the ServiceName) with newName. This rebuilds the dedup index since
// Key() includes Source.
func (ds *DependencySet) RenameSource(oldName, newName string) {
	ds.ServiceName = newName
	rebuilt := make([]NetworkDependency, 0, len(ds.deps))
	ds.seen = make(map[string]bool)
	for _, dep := range ds.deps {
		if dep.Source == oldName {
			dep.Source = newName
		}
		key := dep.Key()
		if !ds.seen[key] {
			ds.seen[key] = true
			rebuilt = append(rebuilt, dep)
		}
	}
	ds.deps = rebuilt
}

// dependencySetJSON is the JSON wire format for DependencySet, matching the
// evidence JSON output produced by the renderer.
type dependencySetJSON struct {
	Service      string              `json:"service"`
	Generated    string              `json:"generated"`
	Version      string              `json:"version"`
	Summary      json.RawMessage     `json:"summary,omitempty"`
	Dependencies []NetworkDependency `json:"dependencies"`
}

// MarshalJSON produces the evidence JSON format.
func (ds *DependencySet) MarshalJSON() ([]byte, error) {
	return json.Marshal(dependencySetJSON{
		Service:      ds.ServiceName,
		Generated:    time.Now().Format("2006-01-02"),
		Version:      "0.5.0",
		Dependencies: ds.Dependencies(),
	})
}

// UnmarshalJSON reads the evidence JSON format and rebuilds the DependencySet.
func (ds *DependencySet) UnmarshalJSON(data []byte) error {
	var raw dependencySetJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ds.ServiceName = raw.Service
	ds.deps = make([]NetworkDependency, 0)
	ds.seen = make(map[string]bool)
	for _, dep := range raw.Dependencies {
		ds.Add(dep)
	}
	return nil
}

package model

import (
	"testing"
)

func TestDiffSetsIdentical(t *testing.T) {
	baseline := NewDependencySet("svc")
	current := NewDependencySet("svc")

	dep := NetworkDependency{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: High}
	baseline.Add(dep)
	current.Add(dep)

	diff := DiffSets(baseline, current)

	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffSetsAdded(t *testing.T) {
	baseline := NewDependencySet("svc")
	current := NewDependencySet("svc")

	shared := NetworkDependency{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: High}
	baseline.Add(shared)
	current.Add(shared)

	extra := NetworkDependency{Source: "web", Target: "redis", Port: 6379, Protocol: "TCP", Confidence: Medium}
	current.Add(extra)

	diff := DiffSets(baseline, current)

	if len(diff.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(diff.Added))
	}
	if diff.Added[0].Target != "redis" {
		t.Errorf("added dep target = %q, want redis", diff.Added[0].Target)
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffSetsRemoved(t *testing.T) {
	baseline := NewDependencySet("svc")
	current := NewDependencySet("svc")

	shared := NetworkDependency{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: High}
	baseline.Add(shared)
	current.Add(shared)

	old := NetworkDependency{Source: "web", Target: "memcached", Port: 11211, Protocol: "TCP", Confidence: High, SourceFile: "docker-compose.yml"}
	baseline.Add(old)

	diff := DiffSets(baseline, current)

	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(diff.Removed))
	}
	if diff.Removed[0].Target != "memcached" {
		t.Errorf("removed dep target = %q, want memcached", diff.Removed[0].Target)
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffSetsMixed(t *testing.T) {
	baseline := NewDependencySet("svc")
	current := NewDependencySet("svc")

	// Shared
	shared := NetworkDependency{Source: "web", Target: "postgres", Port: 5432, Protocol: "TCP", Confidence: High}
	baseline.Add(shared)
	current.Add(shared)

	// Removed
	old := NetworkDependency{Source: "web", Target: "memcached", Port: 11211, Protocol: "TCP", Confidence: High}
	baseline.Add(old)

	// Added
	added := NetworkDependency{Source: "web", Target: "elasticsearch", Port: 9200, Protocol: "TCP", Confidence: Medium}
	current.Add(added)

	diff := DiffSets(baseline, current)

	if len(diff.Added) != 1 {
		t.Errorf("expected 1 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 1 {
		t.Errorf("expected 1 removed, got %d", len(diff.Removed))
	}
	if len(diff.Unchanged) != 1 {
		t.Errorf("expected 1 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffSetsEmpty(t *testing.T) {
	baseline := NewDependencySet("svc")
	current := NewDependencySet("svc")

	diff := DiffSets(baseline, current)

	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Unchanged) != 0 {
		t.Errorf("expected 0 unchanged, got %d", len(diff.Unchanged))
	}
}

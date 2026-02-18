package model

import (
	"testing"
)

func TestNetworkDependencyKey(t *testing.T) {
	dep := NetworkDependency{
		Source:   "order-service",
		Target:   "postgres",
		Port:     5432,
		Protocol: "TCP",
	}
	want := "order-service->postgres:5432/TCP"
	if got := dep.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestDependencySetAdd(t *testing.T) {
	ds := NewDependencySet("my-app")

	dep1 := NetworkDependency{Source: "my-app", Target: "redis", Port: 6379, Protocol: "TCP"}
	dep2 := NetworkDependency{Source: "my-app", Target: "postgres", Port: 5432, Protocol: "TCP"}

	ds.Add(dep1)
	ds.Add(dep2)

	if ds.Len() != 2 {
		t.Errorf("Len() = %d, want 2", ds.Len())
	}
}

func TestDependencySetDeduplication(t *testing.T) {
	ds := NewDependencySet("my-app")

	dep := NetworkDependency{Source: "my-app", Target: "redis", Port: 6379, Protocol: "TCP"}
	ds.Add(dep)
	ds.Add(dep) // duplicate

	if ds.Len() != 1 {
		t.Errorf("Len() = %d after duplicate add, want 1", ds.Len())
	}
}

func TestDependencySetMerge(t *testing.T) {
	ds1 := NewDependencySet("svc-a")
	ds2 := NewDependencySet("svc-b")

	ds1.Add(NetworkDependency{Source: "svc-a", Target: "redis", Port: 6379, Protocol: "TCP"})
	ds2.Add(NetworkDependency{Source: "svc-b", Target: "postgres", Port: 5432, Protocol: "TCP"})
	ds2.Add(NetworkDependency{Source: "svc-a", Target: "redis", Port: 6379, Protocol: "TCP"}) // overlap

	ds1.Merge(ds2)

	if ds1.Len() != 2 {
		t.Errorf("Len() after merge = %d, want 2 (deduped)", ds1.Len())
	}
}

func TestDependenciesSorted(t *testing.T) {
	ds := NewDependencySet("app")
	ds.Add(NetworkDependency{Source: "app", Target: "zookeeper", Port: 2181, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "app", Target: "kafka", Port: 9092, Protocol: "TCP"})
	ds.Add(NetworkDependency{Source: "app", Target: "postgres", Port: 5432, Protocol: "TCP"})

	deps := ds.Dependencies()
	if len(deps) != 3 {
		t.Fatalf("Dependencies() returned %d items, want 3", len(deps))
	}
	// Should be sorted by Key(): kafka, postgres, zookeeper
	if deps[0].Target != "kafka" {
		t.Errorf("first dep target = %q, want kafka", deps[0].Target)
	}
	if deps[1].Target != "postgres" {
		t.Errorf("second dep target = %q, want postgres", deps[1].Target)
	}
	if deps[2].Target != "zookeeper" {
		t.Errorf("third dep target = %q, want zookeeper", deps[2].Target)
	}
}

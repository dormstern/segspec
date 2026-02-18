package tui

import (
	"testing"

	"github.com/dormstern/segspec/internal/model"
)

func TestNewPicker(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
		{Source: "frontend", Target: "adservice", Port: 9555, Protocol: "TCP", Confidence: model.Medium},
	}

	p := NewPicker(deps)
	if len(p.items) != 2 {
		t.Fatalf("got %d items, want 2", len(p.items))
	}
	for i, item := range p.items {
		if !item.selected {
			t.Errorf("item[%d] should be selected by default", i)
		}
	}
}

func TestPicker_Toggle(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
	}
	p := NewPicker(deps)
	p.toggle(0)
	if p.items[0].selected {
		t.Error("item should be deselected after toggle")
	}
	p.toggle(0)
	if !p.items[0].selected {
		t.Error("item should be selected after second toggle")
	}
}

func TestPicker_SelectAll(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "a", Target: "b", Port: 80, Protocol: "TCP", Confidence: model.High},
		{Source: "c", Target: "d", Port: 443, Protocol: "TCP", Confidence: model.Low},
	}
	p := NewPicker(deps)
	p.selectNone()
	for i, item := range p.items {
		if item.selected {
			t.Errorf("item[%d] should be deselected after selectNone", i)
		}
	}
	p.selectAll()
	for i, item := range p.items {
		if !item.selected {
			t.Errorf("item[%d] should be selected after selectAll", i)
		}
	}
}

func TestPicker_Selected(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "frontend", Target: "cartservice", Port: 8080, Protocol: "TCP", Confidence: model.High},
		{Source: "frontend", Target: "adservice", Port: 9555, Protocol: "TCP", Confidence: model.Medium},
		{Source: "cartservice", Target: "redis", Port: 6379, Protocol: "TCP", Confidence: model.High},
	}
	p := NewPicker(deps)
	p.toggle(1) // deselect adservice

	selected := p.Selected()
	if len(selected) != 2 {
		t.Fatalf("got %d selected, want 2", len(selected))
	}
	if selected[0].Target != "cartservice" {
		t.Errorf("selected[0].Target = %q, want cartservice", selected[0].Target)
	}
	if selected[1].Target != "redis" {
		t.Errorf("selected[1].Target = %q, want redis", selected[1].Target)
	}
}

func TestPicker_ToggleOutOfBounds(t *testing.T) {
	deps := []model.NetworkDependency{
		{Source: "a", Target: "b", Port: 80, Protocol: "TCP"},
	}
	p := NewPicker(deps)
	p.toggle(-1) // should not panic
	p.toggle(99) // should not panic
	if !p.items[0].selected {
		t.Error("item should still be selected")
	}
}

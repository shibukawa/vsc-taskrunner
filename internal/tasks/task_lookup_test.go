package tasks

import (
	"encoding/json"
	"testing"
)

func TestCatalogLookupTaskPrefersExactMatch(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"build":    {Label: "build"},
			"go-build": {Label: "go-build", Group: json.RawMessage(`{"kind":"build","isDefault":true}`)},
		},
		Order: []string{"build", "go-build"},
	}

	lookup := catalog.LookupTask("build")
	if lookup.Label != "build" {
		t.Fatalf("label = %q, want build", lookup.Label)
	}
}

func TestCatalogLookupTaskUsesDefaultBuildGroup(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"go-build": {Label: "go-build", Group: json.RawMessage(`{"kind":"build","isDefault":true}`)},
		},
		Order: []string{"go-build"},
	}

	lookup := catalog.LookupTask("build")
	if lookup.Label != "go-build" {
		t.Fatalf("label = %q, want go-build", lookup.Label)
	}
}

func TestCatalogLookupTaskReturnsBuildCandidatesWithoutDefault(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"go-build":  {Label: "go-build", Group: json.RawMessage(`"build"`)},
			"npm-build": {Label: "npm-build", Group: json.RawMessage(`"build"`)},
		},
		Order: []string{"go-build", "npm-build"},
	}

	lookup := catalog.LookupTask("build")
	if lookup.Label != "" {
		t.Fatalf("label = %q, want empty", lookup.Label)
	}
	if len(lookup.Candidates) != 2 {
		t.Fatalf("candidates = %v", lookup.Candidates)
	}
}

func TestCatalogLookupTaskUsesUniqueActionAlias(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"npm-lint": {Label: "npm-lint"},
		},
		Order: []string{"npm-lint"},
	}

	lookup := catalog.LookupTask("lint")
	if lookup.Label != "npm-lint" {
		t.Fatalf("label = %q, want npm-lint", lookup.Label)
	}
}

func TestCatalogLookupTaskReturnsAmbiguousActionAliasCandidates(t *testing.T) {
	t.Parallel()

	catalog := &Catalog{
		Tasks: map[string]ResolvedTask{
			"go-lint":  {Label: "go-lint"},
			"npm-lint": {Label: "npm-lint"},
		},
		Order: []string{"go-lint", "npm-lint"},
	}

	lookup := catalog.LookupTask("lint")
	if lookup.Label != "" {
		t.Fatalf("label = %q, want empty", lookup.Label)
	}
	if len(lookup.Candidates) != 2 {
		t.Fatalf("candidates = %v", lookup.Candidates)
	}
}

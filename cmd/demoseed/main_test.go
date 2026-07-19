package main

import "testing"

func TestDemoReferencesAreUnique(t *testing.T) {
	seen := make(map[string]struct{}, len(demoRefs))
	for _, ref := range demoRefs {
		if ref == "" {
			t.Fatal("demo reference must not be empty")
		}
		if _, exists := seen[ref]; exists {
			t.Fatalf("duplicate demo reference %q", ref)
		}
		seen[ref] = struct{}{}
	}
}

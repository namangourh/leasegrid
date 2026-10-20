package lease

import (
	"slices"
	"testing"
)

func TestFilter(t *testing.T) {
	a1 := Lease{ID: "a1", Provider: "akash", Name: "web", Status: StatusActive}
	a2 := Lease{ID: "a2", Provider: "akash", Name: "db", Status: StatusClosed}
	f1 := Lease{ID: "f1", Provider: "flux", Name: "api", Status: StatusActive}
	all := []Lease{a1, a2, f1}

	tests := []struct {
		name     string
		leases   []Lease
		criteria Criteria
		want     []Lease
	}{
		{name: "zero criteria matches all", leases: all, criteria: Criteria{}, want: all},
		{name: "by provider", leases: all, criteria: Criteria{Provider: "akash"}, want: []Lease{a1, a2}},
		{name: "by status", leases: all, criteria: Criteria{Status: StatusActive}, want: []Lease{a1, f1}},
		{name: "provider and status", leases: all, criteria: Criteria{Provider: "akash", Status: StatusActive}, want: []Lease{a1}},
		{name: "no match", leases: all, criteria: Criteria{Provider: "render"}, want: []Lease{}},
		{name: "nil input", leases: nil, criteria: Criteria{}, want: []Lease{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Filter(tt.leases, tt.criteria)
			if got == nil {
				t.Fatal("Filter returned nil, want a non-nil slice")
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Filter() = %v, want %v", got, tt.want)
			}
		})
	}
}

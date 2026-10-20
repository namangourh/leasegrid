package lease

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestValidate(t *testing.T) {
	valid := Lease{
		ID:       "1001/1/1",
		Provider: "akash",
		Status:   StatusActive,
		Cost:     Cost{PerHour: 3000, Denom: "uakt"},
	}

	tests := []struct {
		name    string
		modify  func(l *Lease) // changes a copy of valid
		wantErr error          // nil means the lease is valid
	}{
		{name: "valid active lease", modify: func(*Lease) {}},
		{name: "valid closed lease", modify: func(l *Lease) { l.Status = StatusClosed }},
		{name: "valid insufficient-funds lease", modify: func(l *Lease) { l.Status = StatusInsufficientFunds }},
		{name: "zero cost is valid", modify: func(l *Lease) { l.Cost = Cost{} }},
		{name: "empty ID", modify: func(l *Lease) { l.ID = "" }, wantErr: ErrMissingID},
		{name: "missing provider name", modify: func(l *Lease) { l.Provider = "" }, wantErr: ErrMissingProvider},
		{name: "empty status", modify: func(l *Lease) { l.Status = "" }, wantErr: ErrUnknownStatus},
		{name: "unsupported status", modify: func(l *Lease) { l.Status = "expiring" }, wantErr: ErrUnknownStatus},
		{name: "negative cost", modify: func(l *Lease) { l.Cost.PerHour = -1 }, wantErr: ErrNegativeCost},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := valid // a copy: assigning a struct copies its fields
			tt.modify(&l)

			err := l.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("validate lease %q", l.ID)) {
				t.Errorf("Validate() error = %q, want it to name the lease", err)
			}
		})
	}
}

func TestValidateMessage(t *testing.T) {
	l := Lease{ID: "x", Provider: "akash", Status: "pending"}
	want := `validate lease "x": unsupported status "pending"`
	if err := l.Validate(); err == nil || err.Error() != want {
		t.Fatalf("Validate() error = %v, want %q", err, want)
	}
}

func TestExpiresWithin(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{name: "unknown expiry", expires: time.Time{}, want: false},
		{name: "inside the window", expires: now.Add(time.Hour), want: true},
		{name: "after the window", expires: now.Add(48 * time.Hour), want: false},
		{name: "already expired", expires: now.Add(-time.Hour), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := Lease{Expires: tt.expires}
			if got := l.ExpiresWithin(now, 24*time.Hour); got != tt.want {
				t.Errorf("ExpiresWithin() = %v, want %v", got, tt.want)
			}
		})
	}
}

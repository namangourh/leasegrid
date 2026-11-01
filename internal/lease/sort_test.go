package lease

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestSort(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	// fluxSoon and akashSoon share both an ID and an expiry, so ties must
	// fall through to the provider-name tie-breaker.
	fluxSoon := Lease{ID: "2", Provider: "flux", Status: StatusActive, Expires: now.Add(time.Hour)}
	akashSoon := Lease{ID: "2", Provider: "akash", Status: StatusInsufficientFunds, Expires: now.Add(time.Hour)}
	fluxLater := Lease{ID: "1", Provider: "flux", Status: StatusActive, Expires: now.Add(48 * time.Hour)}
	akashClosed := Lease{ID: "3", Provider: "akash", Status: StatusClosed} // unknown expiry
	input := []Lease{akashClosed, fluxLater, fluxSoon, akashSoon}

	tests := []struct {
		key  SortKey
		want []Lease
	}{
		{key: SortByID, want: []Lease{fluxLater, akashSoon, fluxSoon, akashClosed}},
		{key: SortByProvider, want: []Lease{akashSoon, akashClosed, fluxLater, fluxSoon}},
		{key: SortByStatus, want: []Lease{fluxLater, fluxSoon, akashSoon, akashClosed}},
		{key: SortByExpires, want: []Lease{akashSoon, fluxSoon, fluxLater, akashClosed}},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			original := slices.Clone(input)

			got, err := Sort(input, tt.key)
			if err != nil {
				t.Fatalf("Sort() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Sort() = %v, want %v", got, tt.want)
			}
			if !slices.Equal(input, original) {
				t.Errorf("Sort changed its input to %v", input)
			}
		})
	}
}

func TestSortKeepsInputOrderForEqualLeases(t *testing.T) {
	// Same ID, provider and expiry: only Name tells them apart.
	first := Lease{ID: "1", Provider: "akash", Name: "first"}
	second := Lease{ID: "1", Provider: "akash", Name: "second"}

	got, err := Sort([]Lease{first, second}, SortByExpires)
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if want := []Lease{first, second}; !slices.Equal(got, want) {
		t.Errorf("Sort() = %v, want %v", got, want)
	}
}

func TestSortEmpty(t *testing.T) {
	got, err := Sort(nil, SortByID)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Sort(nil) = %v, %v; want an empty, non-nil slice", got, err)
	}
}

func TestSortUnknownKey(t *testing.T) {
	got, err := Sort([]Lease{{ID: "1"}}, "cost")
	if !errors.Is(err, ErrUnknownSortKey) || got != nil {
		t.Fatalf("Sort() = %v, %v; want nil, ErrUnknownSortKey", got, err)
	}
}

func TestParseSortKey(t *testing.T) {
	for _, s := range []string{"id", "provider", "status", "expires"} {
		if k, err := ParseSortKey(s); err != nil || string(k) != s {
			t.Errorf("ParseSortKey(%q) = %q, %v; want %q, nil", s, k, err, s)
		}
	}

	_, err := ParseSortKey("cost")
	want := `unknown sort key "cost" (supported: id, provider, status, expires)`
	if !errors.Is(err, ErrUnknownSortKey) || err.Error() != want {
		t.Errorf("ParseSortKey(\"cost\") error = %v, want %q", err, want)
	}
}

func TestCompareExpiry(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	var unknown time.Time // the zero value means an unknown expiry

	tests := []struct {
		name string
		a, b time.Time
		want int
	}{
		{name: "earlier first", a: now, b: later, want: -1},
		{name: "later second", a: later, b: now, want: 1},
		{name: "equal", a: now, b: now, want: 0},
		{name: "unknown after known", a: unknown, b: now, want: 1},
		{name: "known before unknown", a: now, b: unknown, want: -1},
		{name: "both unknown", a: unknown, b: unknown, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareExpiry(tt.a, tt.b); got != tt.want {
				t.Errorf("compareExpiry() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSortByStatusPutsUnknownStatusesLast(t *testing.T) {
	// The app rejects unknown statuses before sorting, but Sort must still
	// order them deterministically if it ever sees one.
	pending := Lease{ID: "1", Provider: "akash", Status: "pending"}
	closed := Lease{ID: "2", Provider: "akash", Status: StatusClosed}

	got, err := Sort([]Lease{pending, closed}, SortByStatus)
	if err != nil {
		t.Fatalf("Sort() error = %v", err)
	}
	if want := []Lease{closed, pending}; !slices.Equal(got, want) {
		t.Errorf("Sort() = %v, want %v", got, want)
	}
}

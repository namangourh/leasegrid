package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/namangourh/leasegrid/internal/app"
	"github.com/namangourh/leasegrid/internal/lease"
	"github.com/namangourh/leasegrid/internal/providers"
	"github.com/namangourh/leasegrid/internal/providers/fake"
)

func TestServiceFindLease(t *testing.T) {
	a1 := lease.Lease{ID: "1001/1/1", Provider: "a", Name: "web", Status: lease.StatusActive}
	a2 := lease.Lease{ID: "1002/1/1", Provider: "a", Name: "api", Status: lease.StatusActive}
	b1 := lease.Lease{ID: "1001/1/1", Provider: "b", Name: "worker", Status: lease.StatusClosed}
	b3 := lease.Lease{ID: "2001/1/1", Provider: "b", Name: "gpu", Status: lease.StatusActive}
	errDown := errors.New("connection refused")

	// Fakes count their calls, so every case builds new ones.
	a := func() *fake.Provider { return &fake.Provider{ProviderName: "a", Leases: []lease.Lease{a1, a2}} }
	b := func() *fake.Provider { return &fake.Provider{ProviderName: "b", Leases: []lease.Lease{b1, b3}} }

	tests := []struct {
		name        string
		providers   []*fake.Provider
		id          string
		provider    string // empty searches every provider
		want        lease.Lease
		wantErr     error    // matched with errors.Is
		wantFoundIn []string // for an *AmbiguousLeaseError: the providers it names
		wantMsg     string   // the exact error text, if set
		wantCalls   []int    // ListLeases calls per provider, in order
	}{
		{
			name:      "match in the first provider",
			providers: []*fake.Provider{a(), b()},
			id:        "1002/1/1",
			want:      a2,
			wantCalls: []int{1, 1},
		},
		{
			name:      "match in the second provider",
			providers: []*fake.Provider{a(), b()},
			id:        "2001/1/1",
			want:      b3,
			wantCalls: []int{1, 1},
		},
		{
			name:        "same ID in two providers is ambiguous",
			providers:   []*fake.Provider{b(), a()},
			id:          "1001/1/1",
			wantFoundIn: []string{"a", "b"},
			wantMsg:     `lease "1001/1/1" is ambiguous (found in: a, b)`,
			wantCalls:   []int{1, 1},
		},
		{
			name:      "provider resolves an ambiguous ID",
			providers: []*fake.Provider{a(), b()},
			id:        "1001/1/1",
			provider:  "b",
			want:      b1,
			wantCalls: []int{0, 1},
		},
		{
			name:      "missing ID",
			providers: []*fake.Provider{a(), b()},
			id:        "9999/1/1",
			wantErr:   app.ErrLeaseNotFound,
			wantMsg:   `lease "9999/1/1" not found (searched: a, b)`,
			wantCalls: []int{1, 1},
		},
		{
			name:      "ID only in a provider that wasn't searched",
			providers: []*fake.Provider{a(), b()},
			id:        "2001/1/1",
			provider:  "a",
			wantErr:   app.ErrLeaseNotFound,
			wantMsg:   `lease "2001/1/1" not found (searched: a)`,
			wantCalls: []int{1, 0},
		},
		{
			name:      "provider with no leases",
			providers: []*fake.Provider{{ProviderName: "empty"}},
			id:        "1001/1/1",
			wantErr:   app.ErrLeaseNotFound,
			wantMsg:   `lease "1001/1/1" not found (searched: empty)`,
			wantCalls: []int{1},
		},
		{
			// down might have the ID too, so a match in a isn't enough.
			name:      "provider error",
			providers: []*fake.Provider{a(), {ProviderName: "down", Err: errDown}},
			id:        "1002/1/1",
			wantErr:   errDown,
			wantMsg:   "provider down: connection refused",
			wantCalls: []int{1, 1},
		},
		{
			name:      "same ID twice in one provider",
			providers: []*fake.Provider{{ProviderName: "a", Leases: []lease.Lease{a1, a2, a1}}},
			id:        "1001/1/1",
			wantErr:   providers.ErrBadResponse,
			wantMsg:   `provider a: bad response: lease "1001/1/1" appears more than once`,
			wantCalls: []int{1},
		},
		{
			name:      "unknown provider",
			providers: []*fake.Provider{a(), b()},
			id:        "1001/1/1",
			provider:  "c",
			wantErr:   app.ErrUnknownProvider,
			wantCalls: []int{0, 0},
		},
		{
			name:      "empty ID",
			providers: []*fake.Provider{a(), b()},
			id:        "",
			wantErr:   app.ErrInvalidLeaseID,
			wantMsg:   "invalid lease ID: it must not be empty",
			wantCalls: []int{0, 0},
		},
		{
			name:    "no providers",
			id:      "1001/1/1",
			wantErr: app.ErrNoProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A []*fake.Provider isn't a []app.Provider, so copy each one.
			ps := make([]app.Provider, len(tt.providers))
			for i, p := range tt.providers {
				ps[i] = p
			}

			got, err := app.New(ps...).FindLease(t.Context(), tt.id, tt.provider)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("FindLease() error = %v, want %v", err, tt.wantErr)
				}
			case tt.wantFoundIn != nil:
				var ambiguous *app.AmbiguousLeaseError
				if !errors.As(err, &ambiguous) {
					t.Fatalf("FindLease() error = %v, want an *AmbiguousLeaseError", err)
				}
				if !slices.Equal(ambiguous.Providers, tt.wantFoundIn) {
					t.Errorf("Providers = %v, want %v", ambiguous.Providers, tt.wantFoundIn)
				}
			default:
				if err != nil {
					t.Fatalf("FindLease() error = %v", err)
				}
			}
			if tt.wantMsg != "" && err.Error() != tt.wantMsg {
				t.Errorf("error = %q, want %q", err, tt.wantMsg)
			}
			if got != tt.want {
				t.Errorf("FindLease() = %+v, want %+v", got, tt.want)
			}
			for i, p := range tt.providers {
				if got := p.Calls(); got != tt.wantCalls[i] {
					t.Errorf("provider %s called %d times, want %d", p.Name(), got, tt.wantCalls[i])
				}
			}
		})
	}
}

// TestServiceFindLeaseCancelled checks that a context cancelled before the
// lookup stops it at the first provider, instead of querying them all.
func TestServiceFindLeaseCancelled(t *testing.T) {
	first := &fake.Provider{ProviderName: "first"}
	second := &fake.Provider{ProviderName: "second"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := app.New(first, second).FindLease(ctx, "1001/1/1", "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FindLease() error = %v, want context.Canceled", err)
	}
	if first.Calls() != 1 || second.Calls() != 0 {
		t.Errorf("calls = %d, %d, want 1, 0", first.Calls(), second.Calls())
	}
}

// TestServiceFindLeaseCancellationReachesProvider proves ctx reaches a
// provider call that is already in progress.
func TestServiceFindLeaseCancellationReachesProvider(t *testing.T) {
	slow := fake.NewBlocking("slow")
	svc := app.New(slow)

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)
	go func() {
		_, err := svc.FindLease(ctx, "1001/1/1", "")
		errc <- err
	}()

	<-slow.Started()
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("FindLease() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FindLease did not return after cancel: ctx never reached the provider")
	}
}

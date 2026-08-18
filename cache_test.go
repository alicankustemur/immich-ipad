package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeImmich serves the two endpoints the cache uses. Pages 1..max return one
// asset for the model; beyond that they come back empty. failNext makes the
// next n search calls fail, to simulate Immich restarting.
type fakeImmich struct {
	mu       sync.Mutex
	max      map[string]int
	calls    int
	failNext int
}

func (f *fakeImmich) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/assets/statistics", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]int{"images": 100000})
	})
	mux.HandleFunc("/api/search/metadata", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Page  int    `json:"page"`
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		f.mu.Lock()
		f.calls++
		if f.failNext > 0 {
			f.failNext--
			f.mu.Unlock()
			http.Error(w, "immich is restarting", http.StatusBadGateway)
			return
		}
		max := f.max[body.Model]
		f.mu.Unlock()

		items := "[]"
		if body.Page >= 1 && body.Page <= max {
			items = fmt.Sprintf(`[{"id":"p%d","fileCreatedAt":"2024-01-01T00:00:00.000Z","originalFileName":"IMG_%d.HEIC"}]`, body.Page, body.Page)
		}
		fmt.Fprintf(w, `{"assets":{"items":%s,"nextPage":""}}`, items)
	})
	return mux
}

func newTestCache(t *testing.T, models []string, max map[string]int) (*PhotoCache, *fakeImmich) {
	t.Helper()
	fake := &fakeImmich{max: max}
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	return &PhotoCache{
		maxPages: make(map[string]int),
		shown:    make(map[string]bool),
		client:   srv.Client(),
		cfg:      Config{ImmichURL: srv.URL, DeviceModels: models},
	}, fake
}

func TestRefreshTotalFindsEachModel(t *testing.T) {
	want := map[string]int{"iPhone 14 Pro": 91117, "iPhone XS": 11135}
	c, _ := newTestCache(t, []string{"iPhone 14 Pro", "iPhone XS"}, want)

	if ok := c.refreshTotal(); !ok {
		t.Fatal("refreshTotal reported no usable counts")
	}
	for model, n := range want {
		if got := c.maxPages[model]; got != n {
			t.Errorf("maxPages[%q] = %d, want %d", model, got, n)
		}
	}
}

// The regression: an API failure mid-search must not drop a model to zero.
func TestTransientFailureKeepsPreviousCount(t *testing.T) {
	c, fake := newTestCache(t, []string{"iPhone 14 Pro", "iPhone XS"}, map[string]int{
		"iPhone 14 Pro": 91117, "iPhone XS": 11135,
	})
	if ok := c.refreshTotal(); !ok {
		t.Fatal("initial refresh failed")
	}

	fake.mu.Lock()
	fake.failNext = 1000 // every probe fails from here on
	fake.mu.Unlock()

	c.refreshTotal()

	if got := c.maxPages["iPhone XS"]; got != 11135 {
		t.Errorf("iPhone XS collapsed to %d during an outage, want 11135 retained", got)
	}
	if got := c.maxPages["iPhone 14 Pro"]; got != 91117 {
		t.Errorf("iPhone 14 Pro collapsed to %d during an outage, want 91117 retained", got)
	}
}

// The hourly refresh should cost a few requests, not a full binary search.
func TestRefreshFromKnownCountIsCheap(t *testing.T) {
	c, fake := newTestCache(t, []string{"iPhone 14 Pro"}, map[string]int{"iPhone 14 Pro": 91117})
	c.refreshTotal()

	fake.mu.Lock()
	fake.max["iPhone 14 Pro"] = 91120 // three new photos arrived
	cold := fake.calls
	fake.mu.Unlock()

	c.refreshTotal()

	fake.mu.Lock()
	warm := fake.calls - cold
	fake.mu.Unlock()

	if c.maxPages["iPhone 14 Pro"] != 91120 {
		t.Errorf("maxPages = %d, want 91120", c.maxPages["iPhone 14 Pro"])
	}
	if warm > 8 {
		t.Errorf("warm refresh took %d requests, want <= 8 (cold was %d)", warm, cold)
	}
	t.Logf("cold search: %d requests, warm refresh: %d requests", cold, warm)
}

func TestModelWithNoPhotosIsExcluded(t *testing.T) {
	c, _ := newTestCache(t, []string{"iPhone 14 Pro", "Pixel 9"}, map[string]int{
		"iPhone 14 Pro": 500, "Pixel 9": 0,
	})
	c.refreshTotal()

	if got := c.maxPages["Pixel 9"]; got != 0 {
		t.Errorf("Pixel 9 = %d, want 0", got)
	}
	for i := 0; i < 50; i++ {
		if model, _ := c.pickModel(); model != "iPhone 14 Pro" {
			t.Fatalf("pickModel returned %q, want only iPhone 14 Pro", model)
		}
	}
}

package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		fmt.Fprint(w, `{"tag_name":"v0.9.0","name":"0.9.0","html_url":"https://example.invalid/release","published_at":"2026-11-12T10:00:00Z"}`)
	}))
	defer srv.Close()

	got, err := Latest(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got.Tag != "v0.9.0" || got.URL == "" || got.PublishedAt.IsZero() {
		t.Fatalf("release = %+v", got)
	}
}

func TestLatestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("404 should fail")
	}
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		want            bool
	}{
		{"0.8.0", "v0.8.1", true},
		{"0.8.0", "v0.9.0", true},
		{"v0.8.0", "1.0.0", true},
		{"0.8.0", "v0.8.0", false},
		{"0.8.1", "v0.8.0", false},
		{"0.8.0", "v0.8.0-beta", false},
		{"0.8.0-beta", "v0.8.0", true},
		{"0.8.0", "not-a-version", false},
	} {
		if got := Newer(tc.current, tc.latest); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}

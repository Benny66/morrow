package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want Version
		ok   bool
	}{
		{"1.2.3", Version{1, 2, 3}, true},
		{"v1.2.3", Version{1, 2, 3}, true},
		{"  v01.002.0003 ", Version{1, 2, 3}, true},
		{"0.0.0", Version{0, 0, 0}, true},
		{"1.2", Version{}, false},
		{"1.2.3.4", Version{}, false},
		{"1.2.3-beta", Version{}, false},
		{"1.2.3+build", Version{}, false},
		{"dev", Version{}, false},
		{"", Version{}, false},
		{"1.2.x", Version{}, false},
		{"-1.2.3", Version{}, false},
	}
	for _, tc := range cases {
		got, ok := ParseVersion(tc.in)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("ParseVersion(%q) = %+v,%v; want %+v,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"1.2.4", "1.2.3", true},
		{"1.3.0", "1.2.9", true},
		{"2.0.0", "1.9.9", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.2", "1.2.3", false},
		{"v1.2.4", "v1.2.3", true},
		{"1.2.3", "dev", false},
		{"dev", "1.2.3", false},
		{"", "1.2.3", false},
	}
	for _, tc := range cases {
		if got := IsNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("IsNewer(%q,%q) = %v; want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func TestLatestFromAPISuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/latest" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name":"v1.2.3",
			"body":"修复说明",
			"html_url":"https://github.com/o/r/releases/tag/v1.2.3",
			"draft":false,
			"prerelease":false
		}`))
	}))
	defer srv.Close()

	c := NewClient("o/r", "Morrow/test")
	c.APIBase = srv.URL
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel == nil || rel.Version != "1.2.3" || rel.Notes != "修复说明" || !strings.Contains(rel.URL, "v1.2.3") {
		t.Fatalf("unexpected release: %+v", rel)
	}
}

func TestLatestSkipsDraftAndPrerelease(t *testing.T) {
	for _, payload := range []string{
		`{"tag_name":"v1.2.3","draft":true}`,
		`{"tag_name":"v1.2.3","prerelease":true}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(payload))
		}))
		c := NewClient("o/r", "Morrow/test")
		c.APIBase = srv.URL
		rel, err := c.Latest(context.Background())
		srv.Close()
		if err != nil {
			t.Fatalf("Latest(%s): %v", payload, err)
		}
		if rel != nil {
			t.Fatalf("Latest(%s) = %+v; want nil", payload, rel)
		}
	}
}

func TestLatestFallsBackToRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/o/r/releases/latest":
			http.Error(w, "boom", http.StatusInternalServerError)
		case r.URL.Path == "/o/r/releases/latest":
			w.Header().Set("Location", "/o/r/releases/tag/v1.2.4")
			w.WriteHeader(http.StatusFound)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient("o/r", "Morrow/test")
	c.APIBase = srv.URL
	c.WebBase = srv.URL
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if rel == nil || rel.Version != "1.2.4" || !strings.Contains(rel.URL, "/releases/tag/v1.2.4") {
		t.Fatalf("unexpected release: %+v", rel)
	}
}

func TestLatestReportsBothFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient("o/r", "Morrow/test")
	c.APIBase = srv.URL
	c.WebBase = srv.URL
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("want error when both channels fail")
	} else if !strings.Contains(err.Error(), "GitHub API") || !strings.Contains(err.Error(), "回退") {
		t.Fatalf("error should mention both channels: %v", err)
	}
}

func TestLatestRejectsNonVersionTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"nightly-20260101","html_url":"https://example.test"}`))
	}))
	defer srv.Close()

	c := NewClient("o/r", "Morrow/test")
	c.APIBase = srv.URL
	c.WebBase = srv.URL
	if _, err := c.Latest(context.Background()); err == nil {
		t.Fatal("want error for non-semver tag")
	}
}

package honeycomb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListDerivedColumns(t *testing.T) {
	const apiKey = "test-key"
	var gotPath, gotTeam string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTeam = r.Header.Get(headerTeam)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"abc","alias":"is_slow","expression":"GT($duration_ms, 1000)","description":"slow spans"},
			{"id":"def","alias":"svc","expression":"$service.name","description":""}
		]`))
	}))
	defer srv.Close()

	c := NewClient(apiKey, WithAPIURL(srv.URL))
	cols, err := c.ListDerivedColumns(context.Background(), "my-dataset")
	if err != nil {
		t.Fatalf("ListDerivedColumns: %v", err)
	}

	if gotPath != "/1/derived_columns/my-dataset" {
		t.Errorf("path = %q, want /1/derived_columns/my-dataset", gotPath)
	}
	if gotTeam != apiKey {
		t.Errorf("X-Honeycomb-Team = %q, want %q", gotTeam, apiKey)
	}
	if len(cols) != 2 {
		t.Fatalf("got %d columns, want 2", len(cols))
	}
	if cols[0].Alias != "is_slow" || cols[0].Expression != "GT($duration_ms, 1000)" {
		t.Errorf("unexpected first column: %+v", cols[0])
	}
}

func TestListDerivedColumnsDefaultsToAllDatasets(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient("k", WithAPIURL(srv.URL))
	if _, err := c.ListDerivedColumns(context.Background(), ""); err != nil {
		t.Fatalf("ListDerivedColumns: %v", err)
	}
	if gotPath != "/1/derived_columns/"+AllDatasets {
		t.Errorf("path = %q, want /1/derived_columns/%s", gotPath, AllDatasets)
	}
}

func TestListDerivedColumnsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient("bad", WithAPIURL(srv.URL))
	_, err := c.ListDerivedColumns(context.Background(), "ds")
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
}

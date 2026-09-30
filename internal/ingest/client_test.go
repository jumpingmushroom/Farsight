package ingest

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostGzipJSONWithBearer(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingest/mv/events" || r.Header.Get("Authorization") != "Bearer tok" ||
			r.Header.Get("Content-Encoding") != "gzip" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad request %s %v", r.URL.Path, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		b, _ := io.ReadAll(zr)
		json.Unmarshal(b, &got)
	}))
	defer srv.Close()
	c := New(srv.URL+"/", "mv", "tok")
	if err := c.Post(context.Background(), "events", map[string]any{"events": []int{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if len(got["events"].([]any)) != 2 {
		t.Fatalf("body = %v", got)
	}
}

func TestPostNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "bad token")
	}))
	defer srv.Close()
	err := New(srv.URL, "mv", "x").Post(context.Background(), "events", 1)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "bad token") {
		t.Fatalf("err = %v", err)
	}
}

func TestPostNon2xxIsStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		io.WriteString(w, strings.Repeat("x", 600))
	}))
	defer srv.Close()
	err := New(srv.URL, "mv", "x").Post(context.Background(), "events", 1)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusUnprocessableEntity || len(se.Body) != 512 {
		t.Fatalf("err = %#v, want *StatusError{Code: 422} with a 512-byte body", err)
	}
}

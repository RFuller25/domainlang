package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDocumentationArgs(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		want    int
		wantErr bool
	}{
		{"default", nil, defaultDocsPort, false},
		{"-p space", []string{"-p", "8080"}, 8080, false},
		{"--port space", []string{"--port", "9000"}, 9000, false},
		{"-p equals", []string{"-p=1234"}, 1234, false},
		{"--port equals", []string{"--port=1234"}, 1234, false},
		{"missing value", []string{"-p"}, 0, true},
		{"non-numeric", []string{"-p", "abc"}, 0, true},
		{"out of range", []string{"-p", "70000"}, 0, true},
		{"zero", []string{"-p", "0"}, 0, true},
		{"unknown flag", []string{"--wat"}, 0, true},
		{"stray file arg", []string{"prog.domain"}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseDocumentationArgs(c.args)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, c.wantErr)
			}
			if err == nil && got != c.want {
				t.Fatalf("port = %d, want %d", got, c.want)
			}
		})
	}
}

// TestDocumentationHandlerServesEmbeddedSite proves the site is embedded in the
// binary (not read from disk) and served correctly — the property the NixOS
// binary depends on.
func TestDocumentationHandlerServesEmbeddedSite(t *testing.T) {
	srv := httptest.NewServer(documentationHandler())
	defer srv.Close()

	// "/" must resolve to the single-page index.html.
	body := getBody(t, srv.URL+"/")
	if !strings.Contains(body, "Domain documentation") || !strings.Contains(body, "search") {
		t.Errorf("index.html not served at /:\n%.200s", body)
	}

	// Every Markdown page index.html fetches must be reachable by name.
	for _, page := range []string{"README.md", "getting-started.md", "primitives.md", "optimizer.md"} {
		md := getBody(t, srv.URL+"/"+page)
		if strings.TrimSpace(md) == "" || !strings.HasPrefix(strings.TrimSpace(md), "#") {
			t.Errorf("%s not served as Markdown:\n%.120s", page, md)
		}
	}

	// The two generated data files behind the gallery and the primitive index
	// ship in the binary too — without them those pages render an error box.
	for _, data := range []string{"gallery.json", "primitives.json"} {
		body := getBody(t, srv.URL+"/"+data)
		var parsed []map[string]any
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Errorf("%s is not served as a JSON array: %v", data, err)
			continue
		}
		if len(parsed) == 0 {
			t.Errorf("%s served empty", data)
		}
	}
}

// The site is embedded in the binary, so the binary is the only thing that can
// say which build a reader is looking at. Served statically there is no
// build.json at all and the sidebar panel stays hidden, which is why the site
// treats it as optional.
func TestDocumentationHandlerReportsItsBuild(t *testing.T) {
	srv := httptest.NewServer(documentationHandler())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/build.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if got := res.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
	// A cached stamp would outlive the binary that produced it.
	if got := res.Header.Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var stamp struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
		Go       string `json:"go"`
	}
	if err := json.Unmarshal(body, &stamp); err != nil {
		t.Fatalf("build.json is not JSON: %v\n%s", err, body)
	}
	// Version and Go runtime are always available; VCS details are not stamped
	// by `go test`, so they are not required here.
	if stamp.Version == "" {
		t.Error("build.json reports no version")
	}
	if !strings.HasPrefix(stamp.Go, "go1.") {
		t.Errorf("build.json reports Go %q", stamp.Go)
	}

	// Adding the endpoint must not have shadowed the file server.
	if body := getBody(t, srv.URL+"/"); !strings.Contains(body, "Domain documentation") {
		t.Error("index.html is no longer served at / after mounting build.json")
	}
}

func TestExpansionDocumentationRejectsBadPort(t *testing.T) {
	var out, errBuf strings.Builder
	if code := Expansion([]string{"documentation"}, []string{"-p", "notaport"}, nil, &out, &errBuf); code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errBuf.String(), "invalid port") {
		t.Errorf("missing invalid-port message:\n%s", errBuf.String())
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	return string(b)
}

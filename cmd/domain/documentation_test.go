package main

import (
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s: %v", url, err)
	}
	return string(b)
}

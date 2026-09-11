package main

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestSmoke(t *testing.T) {
	dir := t.TempDir()
	// Use only shell builtins: no Node, network, credentials, or browser needed.
	const fakeInspector = `#!/bin/sh
set -eu
printf '%s\n' "${MCP_AUTO_OPEN_ENABLED:-}" "$@" > "$MCPHINTS_SMOKE_INVOCATION"
printf '%s\n' 'OAuth diagnostic: https://auth.example.test/authorize' >&2
if [ "$MCPHINTS_SMOKE_FAIL" = true ]; then exit 1; fi
printf '%s\n' '{"tools":[
  {"name":"unknown","inputSchema":{"type":"object"}},
  {"name":"read_only","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}},
  {"name":"annotated","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}}
]}'
`
	if err := os.WriteFile(filepath.Join(dir, "npx"), []byte(fakeInspector), 0o700); err != nil {
		t.Fatal(err)
	}
	// Exclude real executables so a regression cannot invoke Inspector or gh.
	t.Setenv("PATH", dir)
	t.Setenv("MCP_AUTO_OPEN_ENABLED", "false")
	invocation := filepath.Join(dir, "invocation")
	t.Setenv("MCPHINTS_SMOKE_INVOCATION", invocation)
	handler := newHandler()

	for _, fail := range []string{"false", "true"} {
		t.Run("inspector_failure="+fail, func(t *testing.T) {
			t.Setenv("MCPHINTS_SMOKE_FAIL", fail)
			const server = "https://api.fastmail.com/mcp"
			form := url.Values{"server": {server}}
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://localhost:8321")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}

			// Protect the CLI contract that permits a browser OAuth round-trip
			// with pipes and avoids Inspector's default 15-second connect timeout.
			got, err := os.ReadFile(invocation)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Join([]string{
				"true", "-y", "@modelcontextprotocol/inspector@2", "--cli",
				"--transport", "http", "--server-url", server,
				"--connect-timeout", "0", "--method", "tools/list", "",
			}, "\n")
			if string(got) != want {
				t.Errorf("Inspector invocation = %q, want %q", got, want)
			}

			body := w.Body.String()
			if fail == "true" {
				if !strings.Contains(body, `<p class="err">inspector failed:`) ||
					!strings.Contains(body, "OAuth diagnostic: https://auth.example.test/authorize") {
					t.Errorf("missing Inspector stderr in error page: %s", body)
				}
				if strings.Contains(body, "<table>") {
					t.Error("failed fetch rendered a results table")
				}
				return
			}
			if strings.Contains(body, `<p class="err">`) {
				t.Fatalf("fetch failed despite valid stdout: %s", body)
			}
			if !strings.Contains(body, "<strong>Server:</strong> <code>"+server+"</code>") {
				t.Error("missing fetched server metadata")
			}
			fetched := regexp.MustCompile(`fetched ([^<]+)</p>`).FindStringSubmatch(body)
			if len(fetched) != 2 {
				t.Fatal("missing fetched timestamp")
			}
			if _, err := time.Parse(time.RFC3339, fetched[1]); err != nil {
				t.Errorf("invalid fetched timestamp: %v", err)
			}

			rows := regexp.MustCompile(`(?s)<tr>\s*<td>(.*?)</td>(.*?)</tr>`).FindAllStringSubmatch(body, -1)
			cases := []struct {
				name  string
				hints []string
			}{
				{"unknown", []string{"assumed:assumed false", "assumed:assumed true", "assumed:assumed false", "assumed:assumed true"}},
				{"read_only", []string{"safe:claimed true", "moot:n/a — read-only", "assumed:assumed false", "assumed:assumed true"}},
				{"annotated", []string{"risk:claimed false", "safe:claimed false", "safe:claimed true", "safe:claimed false"}},
			}
			if len(rows) != len(cases) {
				t.Fatalf("got %d tool rows, want %d: %s", len(rows), len(cases), body)
			}
			badges := regexp.MustCompile(`<span class="b ([^"]+)">([^<]+)</span>`)
			for i, tc := range cases {
				name, _, _ := strings.Cut(rows[i][1], "<")
				if name != tc.name {
					t.Errorf("row %d tool = %q, want %q", i, name, tc.name)
				}
				if none := strings.Contains(rows[i][1], "no annotations at all"); none != (tc.name == "unknown") {
					t.Errorf("%s: missing-annotations label = %v", tc.name, none)
				}
				var hints []string
				for _, badge := range badges.FindAllStringSubmatch(rows[i][2], -1) {
					hints = append(hints, badge[1]+":"+html.UnescapeString(badge[2]))
				}
				if !slices.Equal(hints, tc.hints) {
					t.Errorf("%s hints = %q, want %q", tc.name, hints, tc.hints)
				}
			}
		})
	}
}

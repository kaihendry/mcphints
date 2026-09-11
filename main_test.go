package main

import (
	"encoding/json"
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
  {"name":"read_only","description":"Read the server without changes.","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}},
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
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	originalStderr := os.Stderr
	os.Stderr = stderr
	t.Cleanup(func() {
		os.Stderr = originalStderr
		stderr.Close()
	})

	for _, fail := range []string{"false", "true"} {
		t.Run("inspector_failure="+fail, func(t *testing.T) {
			t.Setenv("MCPHINTS_SMOKE_FAIL", fail)
			const server = "https://api.fastmail.com/mcp"
			w := postForm(t, handler, url.Values{"server": {server}})

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
			terminal, err := os.ReadFile(stderr.Name())
			if err != nil || !strings.Contains(string(terminal), "OAuth diagnostic: https://auth.example.test/authorize") {
				t.Errorf("Inspector diagnostic missing from terminal: %s (%v)", terminal, err)
			}
			if fail == "true" {
				if !strings.Contains(body, `<p class="err">inspector failed:`) ||
					!strings.Contains(body, "OAuth diagnostic: https://auth.example.test/authorize") {
					t.Errorf("missing Inspector stderr in error page: %s", body)
				}
				if strings.Contains(body, `<section class="tool">`) {
					t.Error("failed fetch rendered tool results")
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

			sections := regexp.MustCompile(`(?s)<section class="tool">\s*<h3>(.*?)</h3>(.*?)</section>`).FindAllStringSubmatch(body, -1)
			cases := []struct {
				name  string
				hints []string
			}{
				{"unknown", []string{"assumed:assumed false", "assumed:assumed true", "assumed:assumed false", "assumed:assumed true"}},
				{"read_only", []string{"safe:claimed true", "moot:n/a — read-only", "moot:n/a — read-only", "assumed:assumed true"}},
				{"annotated", []string{"risk:claimed false", "safe:claimed false", "safe:claimed true", "safe:claimed false"}},
			}
			if len(sections) != len(cases) {
				t.Fatalf("got %d tool sections, want %d: %s", len(sections), len(cases), body)
			}
			badges := regexp.MustCompile(`<li><code>([^<]+)</code>:\s*<span class="b ([^"]+)">([^<]+)</span>`)
			for i, tc := range cases {
				name := html.UnescapeString(sections[i][1])
				if name != tc.name {
					t.Errorf("section %d tool = %q, want %q", i, name, tc.name)
				}
				if none := strings.Contains(sections[i][2], "No annotations declared."); none != (tc.name == "unknown") {
					t.Errorf("%s: missing-annotations label = %v", tc.name, none)
				}
				if description := strings.Contains(sections[i][2], `<h4>Description</h4><p class="description">Read the server without changes.</p>`); description != (tc.name == "read_only") {
					t.Errorf("%s: description displayed = %v", tc.name, description)
				}
				var labels, hints []string
				for _, badge := range badges.FindAllStringSubmatch(sections[i][2], -1) {
					labels = append(labels, badge[1])
					hints = append(hints, badge[2]+":"+html.UnescapeString(badge[3]))
				}
				if !slices.Equal(labels, []string{"readOnlyHint", "destructiveHint", "idempotentHint", "openWorldHint"}) {
					t.Errorf("%s hint labels = %q", tc.name, labels)
				}
				if !slices.Equal(hints, tc.hints) {
					t.Errorf("%s hints = %q, want %q", tc.name, hints, tc.hints)
				}
			}
		})
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		tools       int
		err         string
	}{
		{"object", `{"tools":[{"name":"one"}]}`, 1, ""},
		{"array", ` [{"name":"one"}] `, 1, ""},
		{"rpc", `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"one"}]}}`, 1, ""},
		{"empty object", `{"tools":[]}`, 0, ""},
		{"empty array", `[]`, 0, ""},
		{"empty rpc", `{"result":{"tools":[]}}`, 0, ""},
		{"malformed", `{"tools":`, 0, "invalid tools JSON: unexpected end"},
		{"wrong hint type", `{"tools":[{"annotations":{"readOnlyHint":"true"}}]}`, 0, "annotations.readOnlyHint"},
		{"wrong tools type", `{"tools":{}}`, 0, "cannot unmarshal object"},
		{"missing tools", `{}`, 0, "no tools array found"},
		{"null tools", `{"tools":null}`, 0, "no tools array found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parse(tc.input)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil || len(p.Tools) != tc.tools {
				t.Fatalf("tools = %v, error = %v", p.Tools, err)
			}
			if tc.tools == 0 {
				w := postForm(t, newHandler(), url.Values{"payload": {tc.input}})
				if !strings.Contains(w.Body.String(), "No tools returned.") || strings.Contains(w.Body.String(), `<p class="err">`) {
					t.Fatalf("empty list rendered incorrectly: %s", w.Body.String())
				}
			}
		})
	}
}

func TestPublishSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("MCPHINTS_SMOKE_DIR", dir)
	for name, script := range map[string]string{
		"npx": `printf 'unexpected fetch' > "$MCPHINTS_SMOKE_DIR/fetched"; exit 1`,
		"gh": `printf '%s\n' "$@" > "$MCPHINTS_SMOKE_DIR/args"
while IFS= read -r line || [ -n "$line" ]; do printf '%s\n' "$line"; done > "$MCPHINTS_SMOKE_DIR/report.md"
if [ "$MCPHINTS_SMOKE_FAIL" = true ]; then printf 'gist refused' >&2; exit 1; fi
printf 'https://gist.github.com/example/snapshot\n'`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nset -eu\n"+script+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	const server = "https://api.fastmail.com/mcp"
	const fetched = "2020-01-02T03:04:05Z"
	const description = "Read <script>alert(1)</script>.\nSecond line | preserved."
	const source = `{"result":{"server":"` + server + `","fetched":"` + fetched + `","tools":[{"name":"read_only","title":"A \"quoted\" <title>","description":"Read <script>alert(1)</script>.\nSecond line | preserved.","annotations":{"readOnlyHint":true,"destructiveHint":true,"idempotentHint":false}},{"name":"another"}]}}`
	handler := newHandler()
	w := postForm(t, handler, url.Values{"payload": {source}})
	snapshot := reportSnapshot(t, w.Body.String())
	var p payload
	if err := json.Unmarshal([]byte(snapshot), &p); err != nil || p.Server != server || p.Fetched != fetched || len(p.Tools) != 2 {
		t.Fatalf("invalid report snapshot: %s (%v)", snapshot, err)
	}
	if p.Tools[0].Title == nil || *p.Tools[0].Title != `A "quoted" <title>` {
		t.Fatal("snapshot did not preserve escaped content")
	}
	if p.Tools[0].Description != description || !strings.Contains(w.Body.String(), `<p class="description">`+html.EscapeString(description)+"</p>") || strings.Contains(w.Body.String(), "<script>alert(1)</script>") {
		t.Fatal("description was lost or rendered as HTML")
	}
	for _, fail := range []string{"true", "false"} {
		t.Run("gist_failure="+fail, func(t *testing.T) {
			t.Setenv("MCPHINTS_SMOKE_FAIL", fail)
			w := postForm(t, handler, url.Values{
				"action": {"gist"}, "payload": {snapshot}, "sort": {"name"},
				"server": {"https://should-not-fetch.example/mcp"},
			})
			if _, err := os.Stat(filepath.Join(dir, "fetched")); !os.IsNotExist(err) {
				t.Fatal("publishing invoked Inspector")
			}
			md, err := os.ReadFile(filepath.Join(dir, "report.md"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"- **Server:** `" + server + "`", "- **Fetched:** " + fetched,
				"## Tools\n\n### `another`", "### `read_only`", "#### Description\n\n<p>" + strings.ReplaceAll(html.EscapeString(description), "\n", "<br>\n") + "</p>",
				"#### Annotations\n\n- `readOnlyHint`: 🟢 claimed true\n- `destructiveHint`: n/a — read-only\n- `idempotentHint`: n/a — read-only\n- `openWorldHint`: ⚠️ assumed true",
			} {
				if !strings.Contains(string(md), want) {
					t.Errorf("published report missing %q: %s", want, md)
				}
			}
			args, err := os.ReadFile(filepath.Join(dir, "args"))
			wantArgs := "gist\ncreate\n--filename\nmcp-hints.md\n--desc\nMCP tool annotation hints — " + server + "\n-\n"
			if err != nil || string(args) != wantArgs {
				t.Errorf("gist arguments = %q (%v), want %q", args, err, wantArgs)
			}
			if fail == "true" {
				if !strings.Contains(w.Body.String(), "gist refused") || reportSnapshot(t, w.Body.String()) != snapshot || !strings.Contains(w.Body.String(), `<option value="name" selected>`) {
					t.Fatal("failed publish did not preserve the error and snapshot for retry")
				}
			} else if !strings.Contains(w.Body.String(), `href="https://gist.github.com/example/snapshot"`) {
				t.Fatalf("missing published gist link: %s", w.Body.String())
			}
		})
	}
}

func TestSortSnapshot(t *testing.T) {
	// No executables: sorting must never fetch or publish.
	t.Setenv("PATH", t.TempDir())
	const source = `{"server":"https://example.test/mcp","fetched":"2020-01-02T03:04:05Z","tools":[
		{"name":"z_missing"},
		{"name":"b_safe","annotations":{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":true,"openWorldHint":false}},
		{"name":"c_risky","annotations":{"readOnlyHint":false,"destructiveHint":true,"idempotentHint":false,"openWorldHint":true}},
		{"name":"a_readonly","annotations":{"readOnlyHint":true}}
	]}`
	handler := newHandler()
	for _, tc := range []struct {
		key     string
		reverse bool
		want    string
	}{
		{"", false, "z_missing b_safe c_risky a_readonly"},
		{"", true, "a_readonly c_risky b_safe z_missing"},
		{"name", false, "a_readonly b_safe c_risky z_missing"},
		{"name", true, "z_missing c_risky b_safe a_readonly"},
		{"readOnlyHint", false, "z_missing b_safe c_risky a_readonly"},
		{"readOnlyHint", true, "a_readonly z_missing b_safe c_risky"},
		{"destructiveHint", false, "b_safe z_missing c_risky a_readonly"},
		{"destructiveHint", true, "z_missing c_risky b_safe a_readonly"},
		{"idempotentHint", false, "z_missing c_risky b_safe a_readonly"},
		{"idempotentHint", true, "b_safe z_missing c_risky a_readonly"},
		{"openWorldHint", false, "b_safe z_missing c_risky a_readonly"},
		{"openWorldHint", true, "z_missing c_risky a_readonly b_safe"},
	} {
		form := url.Values{"payload": {source}, "sort": {tc.key}, "server": {"https://should-not-fetch.example/mcp"}}
		if tc.reverse {
			form.Set("reverse", "1")
		}
		w := postForm(t, handler, form)
		var names []string
		for _, heading := range regexp.MustCompile(`<h3>([^<]+)</h3>`).FindAllStringSubmatch(w.Body.String(), -1) {
			names = append(names, heading[1])
		}
		if strings.Join(names, " ") != tc.want {
			t.Errorf("sort %q, reverse %v: got %v, want %s", tc.key, tc.reverse, names, tc.want)
		}
		if strings.Contains(w.Body.String(), `<p class="err">`) || !strings.Contains(w.Body.String(), "fetched 2020-01-02T03:04:05Z") {
			t.Fatalf("sorting lost the report: %s", w.Body.String())
		}
		if checked := strings.Contains(w.Body.String(), `name="reverse" value="1" checked`); checked != tc.reverse {
			t.Error("reverse selection was not preserved")
		}
	}
}

func postForm(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://localhost:8321")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	return w
}

func reportSnapshot(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`<input type="hidden" name="payload" value="([^"]*)">`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("missing report snapshot: %s", body)
	}
	return html.UnescapeString(match[1])
}

package main

import (
	"encoding/json"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMetadataSnapshot(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	source, err := os.ReadFile("testdata/metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Result json.RawMessage }
	if err := json.Unmarshal(source, &envelope); err != nil {
		t.Fatal(err)
	}
	decode := func(value string) any {
		t.Helper()
		var result any
		d := json.NewDecoder(strings.NewReader(value))
		d.UseNumber()
		if err := d.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	handler := newHandler()
	w := postForm(t, handler, url.Values{"payload": {string(source)}})
	snapshot := reportSnapshot(t, w.Body.String())
	if !reflect.DeepEqual(decode(snapshot), decode(string(envelope.Result))) {
		t.Fatalf("snapshot lost original fields or numeric precision: %s", snapshot)
	}
	for _, want := range []string{
		`class="tool-title">Send a message</p>`, `class="tool-title">Legacy title</p>`,
		"Send <strong>Markdown</strong> messages.", "<li>Check the destination</li>",
		`<a href="https://example.test/docs">Documentation</a>`,
		"<h4>Parameters</h4>", "(string; required)", "(see schema; required)",
		"<strong>destination</strong>", "Allowed values:", "Default: <code>0</code>", "Default: <code>false</code>",
		"<summary>Input schema</summary>", "<summary>Output schema</summary>",
		"<summary>Tool metadata</summary>", "<summary>Raw tool JSON</summary>",
		"Partial tool list:", "Cache TTL: 0 ms", "Cache scope:", "same authorization context",
		"<summary>Report metadata</summary>", "page-2", "9007199254740993",
	} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("report missing %q", want)
		}
	}
	w = postForm(t, handler, url.Values{"payload": {snapshot}, "sort": {"name"}})
	if reportSnapshot(t, w.Body.String()) != snapshot {
		t.Fatal("sorting altered the snapshot")
	}
	p, err := parse(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var rows []Row
	for i, tool := range p.Tools {
		rows = append(rows, buildRow(tool, i))
	}
	sortRows(rows, "name", false)
	md := markdown(p, rows)
	for _, want := range []string{
		"<p>Send a message</p>", "<p>Legacy title</p>", "<strong>Markdown</strong>",
		"#### Parameters", "<code>destination</code> (string; required)",
		"<summary>Output schema</summary>", "<summary>Tool metadata</summary>",
		"<summary>Raw tool JSON</summary>", "com.example/annotation", "com.example/list-extension", "9007199254740993",
		"Partial tool list:", "**Cache TTL:** 0 ms", "**Cache scope:** private",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("published report missing %q", want)
		}
	}
}

func TestDescriptionMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want, absent []string
	}{
		{"formatting", "# Usage\n\n**Bold** and *emphasis*\n\n- One\n- Two\n\n```sh\nprintf '<value>'\n```", []string{"<h5>Usage</h5>", "<strong>Bold</strong>", "<em>emphasis</em>", "<li>Two</li>", `class="language-sh"`, "&lt;value&gt;"}, []string{"<h1>", "<value>"}},
		{"raw HTML", "<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n<iframe src='https://example.test'></iframe>", nil, []string{"<script", "<img", "<iframe", "onerror="}},
		{"unsafe links", "[run](javascript:alert%281%29) [encoded](jav&#x61;script:alert%281%29) [data](data:text/html,test)", []string{"run</a>", "encoded</a>", "data</a>"}, []string{"href=\"javascript:", "href=\"data:"}},
		{"images", "![diagram](https://example.test/tracker.png) and ![second](data:image/png;base64,abc)", []string{"diagram", "second"}, []string{"<img", "src=", "https://example.test", "data:image"}},
		{"safe links", "[Docs](https://example.test/?x=1&y=2)", []string{`href="https://example.test/?x=1&amp;y=2"`, "Docs</a>"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rendered := string(renderDescription(tc.source))
			for _, want := range tc.want {
				if !strings.Contains(rendered, want) {
					t.Errorf("missing %q in %s", want, rendered)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(rendered, absent) {
					t.Errorf("unsafe/unexpected %q in %s", absent, rendered)
				}
			}
		})
	}
}

// hints: paste MCP tools/list JSON, see which tool annotations are declared
// vs assumed from the spec's conservative defaults.
// https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations
package main

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

//go:embed form.html
var tmplFS embed.FS

type Annotations struct {
	Title           *string `json:"title,omitempty"`
	ReadOnlyHint    *bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool   `json:"openWorldHint,omitempty"`
}

type Tool struct {
	Name        string       `json:"name"`
	Title       *string      `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Annotations *Annotations `json:"annotations"`
}

type Hint struct {
	Name     string
	Value    bool
	Declared bool
	Risky    bool // holds the worst-case value (which is always the default)
	Moot     bool // destructiveHint and idempotentHint when the tool is read-only
}

type Row struct {
	ID          string
	Name        string
	Description string
	None        bool    // no annotations declared at all
	Hints       [4]Hint // readOnly, destructive, idempotent, openWorld
}

func (r Row) Claims() []Hint {
	var claims []Hint
	for _, h := range r.Hints {
		if h.Declared && !h.Moot {
			claims = append(claims, h)
		}
	}
	return claims
}

// The spec's defaults are deliberately the worst case.
var defaults = [4]Hint{
	{Name: "readOnlyHint", Value: false},
	{Name: "destructiveHint", Value: true},
	{Name: "idempotentHint", Value: false},
	{Name: "openWorldHint", Value: true},
}

func buildRow(t Tool, index int) Row {
	r := Row{ID: fmt.Sprintf("tool-%d", index), Name: t.Name, Description: t.Description, None: t.Annotations == nil}
	var ptrs [4]*bool
	if t.Annotations != nil {
		a := t.Annotations
		ptrs = [4]*bool{a.ReadOnlyHint, a.DestructiveHint, a.IdempotentHint, a.OpenWorldHint}
	}
	for i, p := range ptrs {
		h := defaults[i]
		if p != nil {
			h.Value = *p
			h.Declared = true
		}
		h.Risky = h.Value == defaults[i].Value
		r.Hints[i] = h
	}
	if r.Hints[0].Declared && r.Hints[0].Value {
		r.Hints[1].Moot, r.Hints[2].Moot = true, true
	}
	return r
}

func sortRows(rows []Row, key string, reverse bool) {
	if key == "" {
		if reverse {
			slices.Reverse(rows)
		}
		return
	}
	i := slices.IndexFunc(defaults[:], func(h Hint) bool { return h.Name == key })
	if key != "name" && i < 0 {
		return
	}
	rank := func(h Hint) int {
		if h.Moot {
			return 2 // n/a stays last in either direction
		}
		if h.Value != reverse {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(rows, func(a, b Row) int {
		if key == "name" {
			order := strings.Compare(a.Name, b.Name)
			if reverse {
				return -order
			}
			return order
		}
		return cmp.Compare(rank(a.Hints[i]), rank(b.Hints[i]))
	})
}

type payload struct {
	Server  string `json:"server"`
	Fetched string `json:"fetched"`
	Tools   []Tool `json:"tools"`
}

func parse(s string) (payload, error) {
	var response struct {
		payload
		Result *payload `json:"result"`
	}
	var err error
	if strings.HasPrefix(strings.TrimSpace(s), "[") {
		err = json.Unmarshal([]byte(s), &response.Tools)
	} else {
		err = json.Unmarshal([]byte(s), &response)
	}
	if err != nil {
		return payload{}, fmt.Errorf("invalid tools JSON: %w", err)
	}
	p := response.payload
	if response.Result != nil {
		p = *response.Result
	}
	if p.Tools == nil {
		return p, fmt.Errorf(`no tools array found: paste {"tools":[...]}, {"result":{"tools":[...]}}, or a bare array`)
	}
	return p, nil
}

func cell(h Hint) string {
	switch {
	case h.Moot:
		return "n/a — read-only"
	case !h.Declared:
		return fmt.Sprintf("⚠️ assumed %v", h.Value)
	case h.Risky:
		return fmt.Sprintf("🔴 claimed %v", h.Value)
	default:
		return fmt.Sprintf("🟢 claimed %v", h.Value)
	}
}

func markdown(server, fetched string, rows []Row) string {
	var b strings.Builder
	b.WriteString("# MCP tool annotation hints\n\n")
	if server != "" {
		fmt.Fprintf(&b, "- **Server:** `%s`\n", server)
	}
	if fetched != "" {
		fmt.Fprintf(&b, "- **Fetched:** %s\n", fetched)
	}
	fmt.Fprintf(&b, "- **Published:** %s\n\n", time.Now().UTC().Format(time.RFC3339))
	b.WriteString("## Tools\n\n")
	if len(rows) == 0 {
		b.WriteString("No tools returned.\n\n")
	}
	for _, r := range rows {
		fmt.Fprintf(&b, "- <a href=\"#%s\"><code>%s</code></a> — ", r.ID, template.HTMLEscapeString(r.Name))
		var claims []string
		for _, h := range r.Claims() {
			claims = append(claims, fmt.Sprintf("`%s`: %s", h.Name, cell(h)))
		}
		if len(claims) == 0 {
			b.WriteString("No hints declared.")
		} else {
			b.WriteString(strings.Join(claims, "; "))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "<a name=\"%s\"></a>\n\n### `%s`\n\n", r.ID, r.Name)
		if r.Description != "" {
			fmt.Fprintf(&b, "#### Description\n\n<p>%s</p>\n\n",
				strings.ReplaceAll(template.HTMLEscapeString(r.Description), "\n", "<br>\n"))
		}
		b.WriteString("#### Annotations\n\n")
		if r.None {
			b.WriteString("No annotations declared.\n\n")
		}
		for _, h := range r.Hints {
			fmt.Fprintf(&b, "- `%s`: %s\n", h.Name, cell(h))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nAbsent hints (⚠️) are shown at the [spec's conservative defaults](https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations), i.e. the worst case.\n\n")
	b.WriteString("> **Caveat:** annotations are self-declared, unverified hints. Record them as vendor claims, not controls.\n")
	return b.String()
}

// fetchTools does what the Makefile targets did: run the inspector CLI.
// Inspector v2 speaks Streamable HTTP and OAuth itself, so a URL goes straight
// in — no mcp-remote shim. (Passing one as the target broke: the inspector's
// arg parser drops everything from `-y` on, so it spawned a bare `npx`, which
// is an interactive shell, and fed the handshake to it — hence the baffling
// `sh: method:initialize: command not found`.)
func fetchTools(server string) (string, error) {
	args := []string{"-y", "@modelcontextprotocol/inspector@2", "--cli"}
	if strings.HasPrefix(server, "http://") || strings.HasPrefix(server, "https://") {
		// Transport is only auto-detected from a /mcp or /sse path, and the 15s
		// default connect timeout is far too short for a browser OAuth round-trip.
		transport := "http"
		if strings.HasSuffix(server, "/sse") {
			transport = "sse"
		}
		args = append(args, "--transport", transport, "--server-url", server, "--connect-timeout", "0")
	} else {
		args = append(args, strings.Fields(server)...)
	}
	args = append(args, "--method", "tools/list")
	// Long enough to sign in and consent on a first run.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npx", args...)
	// We exec with pipes, so the inspector sees no TTY and would refuse to start
	// interactive OAuth. This says a human *is* here — go ahead and open the
	// browser. Tee stderr so the consent URL is visible while waiting and is
	// still included in the error if the command fails.
	cmd.Env = append(os.Environ(), "MCP_AUTO_OPEN_ENABLED=true")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, io.MultiWriter(&errb, os.Stderr)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("inspector failed: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

func publishGist(md, server string) (string, error) {
	desc := "MCP tool annotation hints"
	if server != "" {
		desc += " — " + server
	}
	cmd := exec.Command("gh", "gist", "create", "--filename", "mcp-hints.md", "--desc", desc, "-")
	cmd.Stdin = strings.NewReader(md)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh gist create failed: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func newHandler() http.Handler {
	tmpl := template.Must(template.ParseFS(tmplFS, "form.html"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This server execs commands; refuse cross-origin form posts.
		if o := r.Header.Get("Origin"); o != "" && o != "http://localhost:8321" && o != "http://127.0.0.1:8321" {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		data := struct {
			Payload     string
			Snapshot    string
			ServerInput string
			Rows        []Row
			Err         string
			Done        bool
			Server      string
			Fetched     string
			GistURL     string
			GistErr     string
			Sort        string
			Reverse     bool
			HintTypes   [4]Hint
		}{HintTypes: defaults}
		if r.Method == http.MethodPost {
			data.Payload = r.FormValue("payload")
			data.ServerInput = strings.TrimSpace(r.FormValue("server"))
			data.Sort = r.FormValue("sort")
			data.Reverse = r.FormValue("reverse") == "1"
			publishing := r.FormValue("action") == "gist"
			fetching := data.ServerInput != "" && !publishing && !r.PostForm.Has("sort")
			raw := data.Payload
			var p payload
			var snapshot []byte
			var err error
			if fetching {
				raw, err = fetchTools(data.ServerInput)
			}
			if err == nil {
				p, err = parse(raw)
			}
			if err == nil {
				if fetching {
					p.Server = data.ServerInput
					p.Fetched = time.Now().UTC().Format(time.RFC3339)
				}
				snapshot, err = json.Marshal(p)
			}
			if err != nil {
				data.Err = err.Error()
			} else {
				data.Snapshot = string(snapshot)
				if fetching {
					data.Payload = data.Snapshot
				}
				data.Server, data.Fetched = p.Server, p.Fetched
				for i, t := range p.Tools {
					data.Rows = append(data.Rows, buildRow(t, i))
				}
				sortRows(data.Rows, data.Sort, data.Reverse)
				data.Done = true
				if publishing {
					data.GistURL, err = publishGist(markdown(p.Server, p.Fetched, data.Rows), p.Server)
					if err != nil {
						data.GistErr = err.Error()
					}
				}
			}
		}
		if err := tmpl.Execute(w, data); err != nil {
			log.Print(err)
		}
	})
}

func main() {
	http.Handle("/", newHandler())
	log.Println("listening on http://localhost:8321")
	log.Fatal(http.ListenAndServe("localhost:8321", nil))
}

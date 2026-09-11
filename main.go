// hints: paste MCP tools/list JSON, see which tool annotations are declared
// vs assumed from the spec's conservative defaults.
// https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations
package main

import (
	"bytes"
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
	Value    bool
	Declared bool
	Risky    bool // holds the worst-case value (which is always the default)
	Moot     bool // destructiveHint and idempotentHint when the tool is read-only
}

type Row struct {
	Name        string
	Description string
	None        bool    // no annotations declared at all
	Hints       [4]Hint // readOnly, destructive, idempotent, openWorld
}

// The spec's defaults are deliberately the worst case.
var defaults = [4]bool{false, true, false, true}

func buildRow(t Tool) Row {
	r := Row{Name: t.Name, Description: t.Description, None: t.Annotations == nil}
	var ptrs [4]*bool
	if t.Annotations != nil {
		a := t.Annotations
		ptrs = [4]*bool{a.ReadOnlyHint, a.DestructiveHint, a.IdempotentHint, a.OpenWorldHint}
	}
	for i, p := range ptrs {
		h := Hint{Value: defaults[i]}
		if p != nil {
			h.Value = *p
			h.Declared = true
		}
		h.Risky = h.Value == defaults[i]
		r.Hints[i] = h
	}
	if r.Hints[0].Declared && r.Hints[0].Value {
		r.Hints[1].Moot, r.Hints[2].Moot = true, true
	}
	return r
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
	b.WriteString("| Tool | readOnlyHint | destructiveHint | idempotentHint | openWorldHint |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			r.Name, cell(r.Hints[0]), cell(r.Hints[1]), cell(r.Hints[2]), cell(r.Hints[3]))
	}
	for _, r := range rows {
		if r.Description != "" {
			fmt.Fprintf(&b, "\n<details>\n<summary>%s — description</summary>\n\n<pre>%s</pre>\n</details>\n",
				template.HTMLEscapeString(r.Name), template.HTMLEscapeString(r.Description))
		}
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
		var data struct {
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
		}
		if r.Method == http.MethodPost {
			data.Payload = r.FormValue("payload")
			data.ServerInput = strings.TrimSpace(r.FormValue("server"))
			publishing := r.FormValue("action") == "gist"
			fetching := data.ServerInput != "" && !publishing
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
				for _, t := range p.Tools {
					data.Rows = append(data.Rows, buildRow(t))
				}
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

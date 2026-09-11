# MCP hints

Paste an MCP server's `tools/list` JSON (or point it at a live server) and see
which [tool annotations](https://modelcontextprotocol.io/specification/2025-11-25/schema#toolannotations) —
`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint` — it
actually declares, versus what the spec's conservative defaults assume when a
hint is missing.

Annotations are self-declared, unverified hints, not enforced controls. This
tool exists to make that gap visible before you decide which MCP tool actions
to allow.

[![Demo](https://s.natalian.org/2026-07-20/mcphintthumb.png)](https://youtu.be/SvhuBZY4Fu0)

## Run

```
go run .
```

Then open http://localhost:8321, and either:

- give it an `http(s)://` MCP server URL or a stdio command, and it fetches
  `tools/list` for you — OAuth is handled by the MCP inspector, which opens a
  browser on first use and caches the token in `~/.mcp-inspector/`, or
- paste `tools/list` JSON directly.

Results can also be published as an unlisted GitHub gist via `gh` — visible
to anyone with the URL, not access-controlled.

## Smoke tests

Run the automated smoke test and static checks:

```sh
go test ./...
go vet ./...
```

The test POSTs to the real handler with a fake `npx` on `PATH`. It checks
missing and explicit annotations, read-only handling, fetched metadata, the
Inspector arguments and OAuth environment setting, and stderr on success and
failure. It needs Go and `/bin/sh`; no Node, network, browser, or credentials.
This covers our integration with Inspector, not a real OAuth exchange or
browser-side HTMX behavior.

Before changing `fetchTools` or updating Inspector, also check Fastmail OAuth
manually. Start the app with a fresh, isolated token store:

```sh
MCP_INSPECTOR_OAUTH_STATE_PATH="$(mktemp -d)/oauth.json" go run .
```

1. Open http://localhost:8321, enter `https://api.fastmail.com/mcp`, and click
   **Analyse**.
2. Confirm the authorization page opens. Wait more than 15 seconds before
   completing authorization, then confirm the tool table appears. Complete
   this within the app's five-minute fetch timeout.
3. Click **Analyse** again and confirm tools appear using cached credentials,
   without another authorization prompt. Keep the same app process running
   so it uses the same temporary token store.

The [Inspector storage override](https://github.com/modelcontextprotocol/inspector/blob/main/clients/cli/README.md#stored-auth-web--cli-handoff)
keeps this check separate from your usual cached credentials. The automated
test uses a fake Inspector, so repeat this manual check when changing the
Inspector version, including updates picked up by the floating `@2` version.

## License

MIT — see [LICENSE](LICENSE).

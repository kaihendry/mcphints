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

- give it an `http(s)://` MCP server URL (OAuth via `mcp-remote`) or a stdio
  command, and it fetches `tools/list` for you, or
- paste `tools/list` JSON directly.

Results can also be published as a secret GitHub gist via `gh`.

## License

MIT — see [LICENSE](LICENSE).

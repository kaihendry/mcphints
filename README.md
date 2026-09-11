# MCP hints

Inspect an MCP server's tools, descriptions, parameters, and
[annotation hints](https://modelcontextprotocol.io/specification/2026-07-28/schema#toolannotations).
Sort by name or hint to help review what the server claims. Hints are
self-declared, not enforced controls; missing hints use conservative defaults.

[![Demo](https://s.natalian.org/2026-07-20/mcphintthumb.png)](https://youtu.be/SvhuBZY4Fu0)

## Run

```sh
go run .
```

Open http://localhost:8321 and enter an MCP server URL or stdio command,
or paste `tools/list` JSON.

Fetching from a server requires Node.js (`npx`). The MCP Inspector handles
OAuth and opens your browser when authorization is needed.

**Publish this report** shares the results as an unlisted GitHub gist using
an authenticated `gh` CLI. Anyone with the gist URL can read it.

## License

[MIT](LICENSE).

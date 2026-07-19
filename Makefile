WRAP = '{server: $$server, fetched: (now|todate), tools: [.tools[] | {name, title: (.title // .annotations.title), annotations}]}'

fastmail:
	@npx @modelcontextprotocol/inspector --cli npx -y mcp-remote https://api.fastmail.com/mcp --method tools/list 2>/dev/null | jq --arg server "https://api.fastmail.com/mcp" $(WRAP)

gmail:
	@npx @modelcontextprotocol/inspector --cli npx -y mcp-remote https://gmailmcp.googleapis.com/mcp/v1 --method tools/list 2>/dev/null | jq --arg server "https://gmailmcp.googleapis.com/mcp/v1" $(WRAP)

awsdocs:
	@npx -y @modelcontextprotocol/inspector --cli uvx awslabs.aws-documentation-mcp-server@latest --method tools/list 2>/dev/null | jq --arg server "uvx awslabs.aws-documentation-mcp-server@latest (stdio)" $(WRAP)

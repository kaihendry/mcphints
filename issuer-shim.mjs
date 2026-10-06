// Preload for MCP Inspector: when RFC 8414 metadata names a different issuer
// than the one it was fetched for, rewrite it to match. Debugging aid only —
// this switches off the client's mix-up attack protection.
const realFetch = globalThis.fetch;
const WK = "/.well-known/oauth-authorization-server";
globalThis.fetch = async (input, init) => {
  const res = await realFetch(input, init);
  const url = new URL(input instanceof Request ? input.url : String(input));
  if (!res.ok || !url.pathname.startsWith(WK)) return res;
  const meta = await res.clone().json().catch(() => null);
  const expected = url.origin + url.pathname.slice(WK.length);
  if (!meta?.issuer || meta.issuer === expected) return res;
  console.error(`issuer-shim: rewriting issuer ${meta.issuer} -> ${expected}`);
  meta.issuer = expected;
  return new Response(JSON.stringify(meta), { status: res.status, headers: { "content-type": "application/json" } });
};

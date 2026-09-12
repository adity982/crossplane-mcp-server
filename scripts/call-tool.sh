#!/bin/sh
# Call one tool over stdio without an MCP client.
#
# Handy when no MCP client is available, and for checking what a tool really
# returns rather than what a model says it returned.
#
#   scripts/call-tool.sh --list
#   scripts/call-tool.sh crossplane_status
#   scripts/call-tool.sh crossplane_managed_resources_list '{"status":"not-ready"}'
#   scripts/call-tool.sh crossplane_resource_tree '{"kind":"XBucket","name":"demo"}'
#
# Set SERVER to test a different binary, and JSON=1 to see the structured
# payload instead of the rendered text.
set -eu

SERVER="${SERVER:-./bin/crossplane-mcp-server}"

if [ -z "${MCP_URL:-}" ] && [ ! -x "$SERVER" ]; then
	echo "no server binary at $SERVER, run 'make build' first" >&2
	echo "or set MCP_URL to reach a server over HTTP" >&2
	exit 1
fi

init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"call-tool.sh","version":"1"}}}'
ready='{"jsonrpc":"2.0","method":"notifications/initialized"}'

case "${1:-}" in
"")
	echo "usage: $0 <tool> [json-arguments]" >&2
	echo "       $0 --list" >&2
	exit 1
	;;
--list)
	request='{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
	filter='select(.id==2) | (.result.tools[]?.name), (.error.message? // empty)'
	;;
*)
	tool="$1"
	arguments='{}'
	if [ "$#" -ge 2 ] && [ -n "$2" ]; then
		arguments="$2"
	fi
	request=$(printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"%s","arguments":%s}}' "$tool" "$arguments")
	if [ "${JSON:-0}" = "1" ]; then
		filter='select(.id==2) | .result.structuredContent // .result.content[0].text // .error.message'
	else
		filter='select(.id==2) | .result.content[0].text // .error.message'
	fi
	;;
esac

if [ -n "${MCP_URL:-}" ]; then
	# Streamable HTTP needs a session: initialize returns an Mcp-Session-Id
	# that every later request has to carry. Replies arrive as server-sent
	# events, so strip the "data: " prefix before handing them to jq.
	session=$(curl -sS -D - -o /dev/null -X POST "$MCP_URL" \
		-H 'Content-Type: application/json' \
		-H 'Accept: application/json, text/event-stream' \
		-d "$init" | awk 'tolower($1) == "mcp-session-id:" { print $2 }' | tr -d '\r')

	if [ -z "$session" ]; then
		echo "no session id returned by $MCP_URL" >&2
		exit 1
	fi

	curl -sS -o /dev/null -X POST "$MCP_URL" \
		-H 'Content-Type: application/json' \
		-H 'Accept: application/json, text/event-stream' \
		-H "Mcp-Session-Id: $session" \
		-d "$ready"

	curl -sS -X POST "$MCP_URL" \
		-H 'Content-Type: application/json' \
		-H 'Accept: application/json, text/event-stream' \
		-H "Mcp-Session-Id: $session" \
		-d "$request" |
		sed -n 's/^data: //p' |
		jq -r "$filter"
	exit 0
fi

# The server exits only when stdin reaches EOF, so a background writer holds
# the pipe open. Both the writer and the server are killed as soon as the
# response lands; waiting for them to finish on their own is what made this
# feel slow. WAIT only caps how long we wait for a slow cluster.
fifo=$(mktemp -u)
out=$(mktemp)
mkfifo "$fifo"
trap 'rm -f "$fifo" "$out"' EXIT

{
	printf '%s\n%s\n%s\n' "$init" "$ready" "$request"
	sleep "${WAIT:-30}"
} >"$fifo" &
writer=$!

"$SERVER" --log-level error <"$fifo" >"$out" 2>/dev/null &
server=$!

deadline=$(($(date +%s) + ${WAIT:-30}))
while :; do
	if jq -e 'select(.id==2)' "$out" >/dev/null 2>&1; then
		break
	fi
	if [ "$(date +%s)" -ge "$deadline" ]; then
		echo "timed out waiting for a response" >&2
		break
	fi
	sleep 0.05
done

kill "$server" "$writer" 2>/dev/null || true
wait "$server" "$writer" 2>/dev/null || true

jq -r "$filter" <"$out"

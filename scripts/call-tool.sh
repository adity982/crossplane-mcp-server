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

if [ ! -x "$SERVER" ]; then
	echo "no server binary at $SERVER, run 'make build' first" >&2
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

# The server shuts down as soon as stdin reaches EOF, so hold the pipe open
# long enough for the response to come back. Raise WAIT for slow clusters.
# Logs go to stderr and would otherwise bury the response.
{
	printf '%s\n%s\n%s\n' "$init" "$ready" "$request"
	sleep "${WAIT:-5}"
} |
	"$SERVER" --log-level error 2>/dev/null |
	jq -r "$filter"

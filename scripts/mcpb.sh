#!/bin/sh
# Build one MCPB bundle per platform from manifest.json.
#
# Each bundle carries a single cross-compiled binary, so its manifest declares
# only the platform that binary actually runs on. Shipping one bundle with all
# three platforms listed would hand Linux and Windows users a macOS binary.
#
# The tool catalog is read out of the server itself rather than hand-written,
# so the schemas registries see always match the schemas clients get.
#
#   scripts/mcpb.sh
#   VERSION=0.1.3 scripts/mcpb.sh
#   MCPB_PLATFORMS="darwin/arm64 linux/amd64" scripts/mcpb.sh
set -eu

BINARY=crossplane-mcp-server
MODULE=github.com/ravibagri5/crossplane-mcp-server
DIST_DIR="${DIST_DIR:-dist}"
STAGE_DIR="$DIST_DIR/mcpb"

VERSION="${VERSION:-$(git describe --tags --dirty --always 2>/dev/null || echo 0.0.0-dev)}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILD_DATE="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

MCPB_PLATFORMS="${MCPB_PLATFORMS:-darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64}"

command -v zip >/dev/null || { echo "zip is required" >&2; exit 1; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 1; }

# The manifest version must be bare semver, but git tags carry a leading v.
MANIFEST_VERSION="${VERSION#v}"

rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

TOOLS_JSON="$STAGE_DIR/tools.json"
go run "./cmd/$BINARY" tools --json > "$TOOLS_JSON"

for target in $MCPB_PLATFORMS; do
	goos="${target%/*}"
	goarch="${target#*/}"

	case "$goos" in
		windows) mcpb_platform=win32; entry_point="$BINARY.exe" ;;
		darwin)  mcpb_platform=darwin; entry_point="$BINARY" ;;
		linux)   mcpb_platform=linux;  entry_point="$BINARY" ;;
		*) echo "unsupported GOOS: $goos" >&2; exit 1 ;;
	esac

	stage="$STAGE_DIR/${goos}_${goarch}"
	mkdir -p "$stage"

	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
		-ldflags "-s -w \
			-X $MODULE/pkg/version.Version=$VERSION \
			-X $MODULE/pkg/version.Commit=$COMMIT \
			-X $MODULE/pkg/version.BuildDate=$BUILD_DATE" \
		-o "$stage/$entry_point" "./cmd/$BINARY"

	cp icon.png "$stage/icon.png"

	MANIFEST_VERSION="$MANIFEST_VERSION" \
	MCPB_PLATFORM="$mcpb_platform" \
	MCPB_ENTRY_POINT="$entry_point" \
	MCPB_STAGE="$stage" \
	MCPB_TOOLS="$TOOLS_JSON" \
	python3 -c '
import json, os

with open("manifest.json") as f:
    m = json.load(f)

with open(os.environ["MCPB_TOOLS"]) as f:
    m["tools"] = json.load(f)

m["version"] = os.environ["MANIFEST_VERSION"]
m["server"]["entry_point"] = os.environ["MCPB_ENTRY_POINT"]
m["server"]["mcp_config"]["command"] = "${__dirname}/" + os.environ["MCPB_ENTRY_POINT"]
m["compatibility"]["platforms"] = [os.environ["MCPB_PLATFORM"]]

with open(os.environ["MCPB_STAGE"] + "/manifest.json", "w") as f:
    json.dump(m, f, indent=2)
    f.write("\n")
'

	bundle="$BINARY-$MANIFEST_VERSION-$goos-$goarch.mcpb"
	rm -f "$DIST_DIR/$bundle"
	(cd "$stage" && zip -q -r -X "../../$bundle" .)
	echo "packed $DIST_DIR/$bundle"
done

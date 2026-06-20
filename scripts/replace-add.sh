#!/usr/bin/env bash
# Adds local replace directives for all github.com/netgarden/maf/* modules.
# Run this after cloning for local development.
# Run replace-remove.sh before tagging a release.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

add() {
    local mod="$1" path="$2"
    (cd "$ROOT/$dir" && go mod edit -replace="${mod}=${path}")
    echo "  ${mod} => ${path}"
}

dir=security
echo "$dir"
add github.com/netgarden/maf ..

dir=logging
echo "$dir"
add github.com/netgarden/maf ..

dir=database
echo "$dir"
add github.com/netgarden/maf ..

dir=auth
echo "$dir"
add github.com/netgarden/maf      ..
add github.com/netgarden/maf/database  ../database
add github.com/netgarden/maf/security  ../security

dir=web
echo "$dir"
add github.com/netgarden/maf     ..
add github.com/netgarden/maf/mergefs  ../mergefs
add github.com/netgarden/maf/config   ../config

dir=datatables
echo "$dir"
add github.com/netgarden/maf     ..
add github.com/netgarden/maf/mergefs  ../mergefs
add github.com/netgarden/maf/web      ../web

dir=rrpc-server
echo "$dir"
add github.com/netgarden/maf      ..
add github.com/netgarden/maf/security  ../security

dir=rrpc-auth
echo "$dir"
add github.com/netgarden/maf      ..
add github.com/netgarden/maf/auth      ../auth
add github.com/netgarden/maf/database   ../database
add github.com/netgarden/maf/security   ../security

echo ""
echo "Done. Local replace directives added."
echo "Note: rrpc-server and rrpc-auth still need a manual replace for github.com/netgarden/rrpc."

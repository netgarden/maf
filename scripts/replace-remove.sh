#!/usr/bin/env bash
# Removes all github.com/netgarden/maf/* local replace directives.
# Run this before tagging a release so go.mod files are clean for consumers.

set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

drop() {
    local mod="$1"
    (cd "$ROOT/$dir" && go mod edit -dropreplace="${mod}")
    echo "  dropped ${mod}"
}

dir=security
echo "$dir"
drop github.com/netgarden/maf

dir=logging
echo "$dir"
drop github.com/netgarden/maf

dir=database
echo "$dir"
drop github.com/netgarden/maf

dir=auth
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/database
drop github.com/netgarden/maf/security

dir=web
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/mergefs
drop github.com/netgarden/maf/config

dir=datatables
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/mergefs
drop github.com/netgarden/maf/web

dir=rrpc-server
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/security

dir=rrpc-auth
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/auth
drop github.com/netgarden/maf/database
drop github.com/netgarden/maf/security

dir=locks
echo "$dir"
drop github.com/netgarden/maf

dir=jobs
echo "$dir"
drop github.com/netgarden/maf
drop github.com/netgarden/maf/database
drop github.com/netgarden/maf/locks

echo ""
echo "Done. Local replace directives removed."
echo "Note: rrpc-server and rrpc-auth may still have a replace for github.com/netgarden/rrpc — remove manually if present."
echo "Note: jobs may still have a replace for github.com/netgarden/orderedlist — remove manually if present."

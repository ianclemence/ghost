#!/usr/bin/env bash
# Preview tooling: start the web console on synthetic data and screenshot pages.
#   scripts/preview/console.sh start          # console :8480 + mock gateway :8392
#   scripts/preview/console.sh shot home out.png [width height]
#   scripts/preview/console.sh stop
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"; W=/tmp/ghost-preview; BIN=$W/ghost-web
case "$1" in
  start)
    rm -rf $W; mkdir -p $W; (cd $ROOT && go run ./scripts/preview/seed $W && go build -o $BIN ./cmd/ghost-web)
    (bun $ROOT/scripts/preview/mockpod.ts > $W/mock.log 2>&1 & echo $! > $W/mock.pid)
    (GHOST_DIR=$W $BIN -port 8480 -dir $W -force > $W/web.log 2>&1 & echo $! > $W/web.pid); sleep 3; echo "console http://127.0.0.1:8480 (password preview-pass-1)";;
  shot)
    R="$2"; OUT="$3"; WD="${4:-1280}"; HT="${5:-800}"
    JS="(async()=>{await fetch('/api/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({password:'preview-pass-1'})}); location.hash='$R'; location.reload();})()"
    mkdir -p "$(dirname "$OUT")"; bun $ROOT/scripts/preview/shot.mjs http://127.0.0.1:8480/ "$OUT" 4000 $WD $HT "$JS";;
  stop) kill $(cat $W/web.pid $W/mock.pid) 2>/dev/null || true;;
esac

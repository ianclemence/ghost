#!/usr/bin/env bash
set -euo pipefail
ROOT="${GHOST_WORKSPACE:-$PWD}"
LEARNING_ROOT="${SELF_IMPROVING_LEARNING_ROOT:-${SELF_IMPROVING_LEARNING_DIR:-$ROOT/learning}}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_DIR="${SELF_IMPROVING_SKILL_DIR:-$(dirname "$SCRIPT_DIR")}"
LEARNINGS_CLI="${SELF_IMPROVING_LEARNINGS_CLI:-$SKILL_DIR/scripts/learnings.py}"
LOG="$LEARNING_ROOT/system-failure-audit.log"
mkdir -p "$LEARNING_ROOT"
{
  echo "# system failure audit $(date -Is)"
  if command -v ghost >/dev/null 2>&1; then
    echo "## ghost status"
    ghost status 2>&1 || true
    echo "## gateway status"
    ghost gateway status 2>&1 || true
    if ghost help 2>/dev/null | grep -q '\bdoctor\b'; then
      echo "## ghost doctor"
      ghost doctor 2>&1 || true
    fi
  else
    echo "ghost CLI not found"
  fi
} > "$LOG.tmp"
mv "$LOG.tmp" "$LOG"
if grep -Eiq '(^|[^a-z])(error|failed|failure|unhealthy|critical|panic|exception|traceback)([^a-z]|$)' "$LOG"; then
  SUMMARY=$(grep -Ei 'error|failed|failure|unhealthy|critical|panic|exception|traceback' "$LOG" | head -5 | tr '\n' '; ' | cut -c1-500)
  python3 "$LEARNINGS_CLI" --root "$ROOT" search "$SUMMARY" --limit 3 | grep -q 'No results' && \
  python3 "$LEARNINGS_CLI" --root "$ROOT" log-error \
    --summary "Ghost system audit detected failure signal" \
    --details "${SUMMARY}. Full audit log: $LOG" \
    --pattern "system:ghost-audit-failure" \
    --area "domain:ghost" \
    --force || true
fi
echo "[system-failure-audit] wrote $LOG"

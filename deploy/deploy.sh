#!/usr/bin/env bash
# Installs a new Talaria binary and restarts the service, keeping the previous
# binary for rollback. Called by CI (via sudo) and usable by hand:
#
#   sudo /opt/talaria/deploy.sh ./talaria-linux-amd64
#   sudo /opt/talaria/deploy.sh --rollback
set -euo pipefail

DIR=/opt/talaria
BIN="$DIR/talaria"

if [[ "${1:-}" == "--rollback" ]]; then
  [[ -f "$BIN.prev" ]] || { echo "no previous binary to roll back to" >&2; exit 1; }
  mv -f "$BIN" "$BIN.bad" && mv -f "$BIN.prev" "$BIN"
  systemctl restart talaria
  echo "rolled back"
  exit 0
fi

SRC="${1:?usage: deploy.sh <new-binary> | --rollback}"
install -m 0755 -o talaria -g talaria "$SRC" "$BIN.new"
"$BIN.new" -version

if [[ -f "$BIN" ]]; then
  mv -f "$BIN" "$BIN.prev"
fi
mv -f "$BIN.new" "$BIN"
systemctl restart talaria

# Wait for the health endpoint; roll back automatically if it never answers.
PORT="$(grep -E '^\s*http_port' "$DIR/talaria.toml" 2>/dev/null | sed -E 's/.*=\s*([0-9]+).*/\1/' || true)"
PORT="${PORT:-8080}"
for i in $(seq 1 20); do
  if curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1; then
    echo "deployed $("$BIN" -version) and healthy on :${PORT}"
    exit 0
  fi
  sleep 1
done

echo "health check failed; rolling back" >&2
journalctl -u talaria -n 30 --no-pager >&2 || true
if [[ -f "$BIN.prev" ]]; then
  mv -f "$BIN" "$BIN.bad" && mv -f "$BIN.prev" "$BIN"
  systemctl restart talaria
fi
exit 1

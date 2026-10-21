#!/usr/bin/env bash
# Deploy a QuantumCash testnet seed node to a fresh Ubuntu/Debian server.
#   scripts/deploy-seed.sh root@1.2.3.4 [api-hostname] [extra node args...]
# Example:
#   scripts/deploy-seed.sh root@1.2.3.4 seed1.quantumcash.network
#   scripts/deploy-seed.sh root@5.6.7.8 seed2.quantumcash.network -mine=false -seeds seed1.quantumcash.network:7101
# Builds the Go binary locally, copies it over SSH, installs a systemd unit,
# opens the p2p port (7101) and, if a hostname is given, HTTPS via Caddy.
set -euo pipefail
TARGET=${1:?usage: deploy-seed.sh user@host [api-hostname] [node args...]}
API_HOST=${2:-}
shift; [ $# -gt 0 ] && shift
EXTRA="$*"
cd "$(dirname "$0")/.."

echo "==> building linux/amd64 binary"
ARCH=$(ssh "$TARGET" uname -m)
case "$ARCH" in x86_64) GOARCH=amd64 ;; aarch64|arm64) GOARCH=arm64 ;; *) echo "unsupported arch $ARCH"; exit 1 ;; esac
CGO_ENABLED=0 GOOS=linux GOARCH=$GOARCH go build -trimpath -ldflags="-s -w" -o /tmp/qcash-$GOARCH ./cmd/qcash

echo "==> copying to $TARGET"
scp -q /tmp/qcash-$GOARCH "$TARGET":/tmp/qcash
scp -q deploy/qcash-seed.service "$TARGET":/tmp/qcash-seed.service
[ -n "$API_HOST" ] && scp -q deploy/Caddyfile "$TARGET":/tmp/Caddyfile

ssh "$TARGET" "API_HOST='$API_HOST' EXTRA='$EXTRA' sudo -E bash -s" <<'REMOTE'
set -euo pipefail
id qcash >/dev/null 2>&1 || useradd --system --home /var/lib/qcash --shell /usr/sbin/nologin qcash
install -d -o qcash -g qcash /var/lib/qcash
install -m 0755 /tmp/qcash /usr/local/bin/qcash
install -m 0644 /tmp/qcash-seed.service /etc/systemd/system/qcash-seed.service
echo "QCASH_ARGS=$EXTRA" > /etc/default/qcash
if command -v ufw >/dev/null; then
  ufw allow OpenSSH >/dev/null; ufw allow 7101/tcp >/dev/null
  [ -n "$API_HOST" ] && { ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null; }
  ufw --force enable >/dev/null
fi
systemctl daemon-reload
systemctl enable --now qcash-seed
systemctl restart qcash-seed
if [ -n "$API_HOST" ]; then
  if ! command -v caddy >/dev/null; then
    apt-get update -qq && apt-get install -y -qq caddy
  fi
  sed "s/{\$QCASH_API_HOST}/$API_HOST/" /tmp/Caddyfile > /etc/caddy/Caddyfile
  systemctl enable --now caddy && systemctl reload caddy
fi
sleep 3
systemctl --no-pager --lines=5 status qcash-seed || true
curl -fsS 127.0.0.1:8645/stats && echo
REMOTE
echo "==> done. p2p: ${TARGET#*@}:7101  API: ${API_HOST:+https://$API_HOST/stats}"

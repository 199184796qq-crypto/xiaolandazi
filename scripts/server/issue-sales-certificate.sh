#!/usr/bin/env bash
set -euo pipefail

DOMAIN="${1:-sales.xiaolandaizi.cn}"
EXPECTED_IP="${2:-47.114.55.117}"
CHALLENGE_ROOT=/var/www/letsencrypt
ACME_CONF=/etc/nginx/conf.d/xiaolan-sales-acme.conf
CERT_ROOT="/etc/letsencrypt/live/$DOMAIN"

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$DOMAIN" "$EXPECTED_IP"
fi

if ! getent ahostsv4 "$DOMAIN" | awk '{print $1}' | grep -Fxq "$EXPECTED_IP"; then
  echo "$DOMAIN does not resolve to $EXPECTED_IP yet" >&2
  exit 2
fi

mkdir -p "$CHALLENGE_ROOT/.well-known/acme-challenge"
cat > "$ACME_CONF" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;

    location /.well-known/acme-challenge/ {
        root $CHALLENGE_ROOT;
    }

    location / {
        return 404;
    }
}
EOF

cleanup() {
  rm -f "$ACME_CONF"
  nginx -t >/dev/null 2>&1 && systemctl reload nginx || true
}
trap cleanup EXIT

nginx -t
systemctl reload nginx

certbot certonly \
  --webroot \
  --webroot-path "$CHALLENGE_ROOT" \
  --domain "$DOMAIN" \
  --non-interactive \
  --agree-tos \
  --keep-until-expiring

[[ -f "$CERT_ROOT/fullchain.pem" ]]
[[ -f "$CERT_ROOT/privkey.pem" ]]

mkdir -p /etc/letsencrypt/renewal-hooks/deploy
cat > /etc/letsencrypt/renewal-hooks/deploy/xiaolan-nginx-reload.sh <<'EOF'
#!/bin/sh
systemctl reload nginx
EOF
chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/xiaolan-nginx-reload.sh

echo "certificate ready: $CERT_ROOT/fullchain.pem"

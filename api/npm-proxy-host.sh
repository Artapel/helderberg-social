#!/usr/bin/env bash
# Create (or verify) the Nginx Proxy Manager host that fronts the API:
#   https://api.helderbergsocial.co.za  ->  http://<HS_BIND_IP>:8102
# and give it a Let's Encrypt certificate (HTTP-01, so the DNS A record must
# already point at the proxy before running this).
#
# Idempotent: re-running finds the existing host/cert and only fixes what is
# missing. Run ON the Docker host, as the user that runs NPM, with:
#   NPM_USER=<admin email> NPM_PW=<password> NPM_EMAIL=<letsencrypt contact> bash api/npm-proxy-host.sh
# The password is read from the environment only; it is never written to disk
# or passed as an argument.
set -euo pipefail

HOST="${HS_API_HOST:-api.helderbergsocial.co.za}"
UPSTREAM_HOST="${HS_BIND_IP:-192.168.111.150}"
UPSTREAM_PORT="${HS_PORT:-8102}"
NPM_URL="${NPM_URL:-http://127.0.0.1:81}"

: "${NPM_USER:?set NPM_USER (NPM admin login email)}"
: "${NPM_PW:?set NPM_PW (NPM admin password) in the environment}"
: "${NPM_EMAIL:?set NPM_EMAIL (Let's Encrypt contact address)}"
command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

# --- token (password goes over the local loopback only) ---------------------
TOKEN=$(jq -n --arg u "$NPM_USER" --arg p "$NPM_PW" '{identity:$u, secret:$p}' \
  | curl -sS --fail -H 'Content-Type: application/json' -d @- "$NPM_URL/api/tokens" | jq -r .token)
[ -n "$TOKEN" ] && [ "$TOKEN" != null ] || { echo "NPM login failed" >&2; exit 1; }
auth=(-H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json')
api() { curl -sS --fail "${auth[@]}" "$@"; }

# --- proxy host --------------------------------------------------------------
HOST_ID=$(api "$NPM_URL/api/nginx/proxy-hosts" | jq -r --arg h "$HOST" '.[] | select(.domain_names | index($h)) | .id' | head -n1)
if [ -z "$HOST_ID" ]; then
  echo "creating proxy host $HOST -> $UPSTREAM_HOST:$UPSTREAM_PORT"
  HOST_ID=$(jq -n --arg h "$HOST" --arg fh "$UPSTREAM_HOST" --argjson fp "$UPSTREAM_PORT" '{
      domain_names: [$h], forward_scheme: "http", forward_host: $fh, forward_port: $fp,
      access_list_id: 0, certificate_id: 0, ssl_forced: false, caching_enabled: false,
      block_exploits: true, allow_websocket_upgrade: false, http2_support: false,
      hsts_enabled: false, hsts_subdomains: false, locations: [],
      advanced_config: "client_max_body_size 64k;\nproxy_set_header X-Real-IP $remote_addr;",
      meta: { letsencrypt_agree: false, dns_challenge: false } }' \
    | api -d @- "$NPM_URL/api/nginx/proxy-hosts" | jq -r .id)
  echo "  host id $HOST_ID"
else
  echo "proxy host exists: id $HOST_ID"
fi

# --- certificate -------------------------------------------------------------
CERT_ID=$(api "$NPM_URL/api/nginx/certificates" | jq -r --arg h "$HOST" '.[] | select(.provider=="letsencrypt" and (.domain_names | index($h))) | .id' | head -n1)
if [ -z "$CERT_ID" ]; then
  echo "requesting Let's Encrypt certificate for $HOST (HTTP-01; can take a minute)"
  CERT_ID=$(jq -n --arg h "$HOST" --arg e "$NPM_EMAIL" '{
      domain_names: [$h], provider: "letsencrypt",
      meta: { letsencrypt_email: $e, letsencrypt_agree: true, dns_challenge: false } }' \
    | curl -sS --fail --max-time 300 "${auth[@]}" -d @- "$NPM_URL/api/nginx/certificates" | jq -r .id)
  echo "  certificate id $CERT_ID"
else
  echo "certificate exists: id $CERT_ID"
fi

# --- bind cert, force TLS -----------------------------------------------------
CUR=$(api "$NPM_URL/api/nginx/proxy-hosts/$HOST_ID")
if [ "$(echo "$CUR" | jq -r .certificate_id)" != "$CERT_ID" ] || [ "$(echo "$CUR" | jq -r .ssl_forced)" != "true" ]; then
  echo "binding certificate and forcing HTTPS"
  echo "$CUR" | jq --argjson c "$CERT_ID" '{
      domain_names, forward_scheme, forward_host, forward_port, access_list_id, block_exploits,
      allow_websocket_upgrade, caching_enabled, advanced_config, locations, meta,
      certificate_id: $c, ssl_forced: true, http2_support: true, hsts_enabled: true, hsts_subdomains: false }' \
    | api -X PUT -d @- "$NPM_URL/api/nginx/proxy-hosts/$HOST_ID" >/dev/null
fi

echo "done. verify:  curl -sS https://$HOST/api/health"

#!/bin/sh
set -eu

TLS_CERT="${TLS_CERT:-/etc/nginx/certs/wildcard.crt}"
TLS_KEY="${TLS_KEY:-/etc/nginx/certs/wildcard.key}"
GENERATED_CONFIG="${NGINX_CONFIG_PATH:-/etc/nginx/conf.d/generated.conf}"
BASE_DOMAIN="${BASE_DOMAIN:-local.dev-gutools.co.uk}"
LOCAL_CA_DIR="${LOCAL_CA_DIR:-/etc/nginx/mkcert}"
LOCAL_CA_CERT="${LOCAL_CA_CERT:-${LOCAL_CA_DIR}/rootCA.pem}"
LOCAL_CA_KEY="${LOCAL_CA_KEY:-${LOCAL_CA_DIR}/rootCA-key.pem}"

mkdir -p "$(dirname "$TLS_CERT")" "$(dirname "$TLS_KEY")" "$(dirname "$GENERATED_CONFIG")"

EFFECTIVE_TLS_CERT="$TLS_CERT"
EFFECTIVE_TLS_KEY="$TLS_KEY"

if [ ! -f "$TLS_CERT" ] || [ ! -f "$TLS_KEY" ]; then
  CERT_DIR="$(dirname "$TLS_CERT")"
  KEY_DIR="$(dirname "$TLS_KEY")"
  DIAG_CERT="${CERT_DIR}/devcontainer-nginx.${BASE_DOMAIN}.crt"
  DIAG_KEY="${KEY_DIR}/devcontainer-nginx.${BASE_DOMAIN}.key"

  if [ -f "$DIAG_CERT" ] && [ -f "$DIAG_KEY" ]; then
    echo "Using host-specific certificate pair for devcontainer-nginx.${BASE_DOMAIN}"
    EFFECTIVE_TLS_CERT="$DIAG_CERT"
    EFFECTIVE_TLS_KEY="$DIAG_KEY"
  else
    if [ -f "$LOCAL_CA_CERT" ] && [ -f "$LOCAL_CA_KEY" ]; then
      # Keep certificate coverage scoped to the current base domain only.
      # This keeps the local TLS trust model simple: a single mkcert root CA,
      # plus one wildcard certificate per BASE_DOMAIN.
      if ! sh -c "touch ${CERT_DIR}/.write-test && rm -f ${CERT_DIR}/.write-test" >/dev/null 2>&1; then
        mkdir -p /tmp/devcontainer-nginx-certs
        EFFECTIVE_TLS_CERT="/tmp/devcontainer-nginx-certs/wildcard.crt"
        EFFECTIVE_TLS_KEY="/tmp/devcontainer-nginx-certs/wildcard.key"
      fi

      echo "Generating wildcard certificate for *.${BASE_DOMAIN} using mkcert"
      CAROOT="$(dirname "$LOCAL_CA_CERT")" mkcert \
        -cert-file "$EFFECTIVE_TLS_CERT" \
        -key-file "$EFFECTIVE_TLS_KEY" \
        "*.${BASE_DOMAIN}" "${BASE_DOMAIN}"
    else
      echo "TLS certificate/key not found; generating self-signed fallback certificate"
      openssl req -x509 -nodes -newkey rsa:2048 -days 365 \
        -subj "/CN=localhost" \
        -keyout "$EFFECTIVE_TLS_KEY" \
        -out "$EFFECTIVE_TLS_CERT" >/dev/null 2>&1
    fi
  fi
fi

cat > "$GENERATED_CONFIG" <<EOF
# Bootstrap config generated at container start.
server {
    listen 80 default_server;
    server_name _;

    location = /health {
        add_header Content-Type text/plain;
        return 200 'ok';
    }

    location / {
        add_header Content-Type text/plain;
        return 503 'No route configured';
    }
}

server {
    listen 443 ssl default_server;
    server_name _;

    ssl_certificate ${EFFECTIVE_TLS_CERT};
    ssl_certificate_key ${EFFECTIVE_TLS_KEY};
    http2 on;

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    location = /health {
        add_header Content-Type text/plain;
        return 200 'ok';
    }

    location / {
        add_header Content-Type text/plain;
        return 503 'No route configured';
    }
}
EOF

nginx -g "daemon off;" &
/usr/local/bin/devcontainer-nginx \
  --base-domain "${BASE_DOMAIN:-local.dev-gutools.co.uk}" \
  --tls-cert "$EFFECTIVE_TLS_CERT" \
  --tls-key "$EFFECTIVE_TLS_KEY" \
  --nginx-config "$GENERATED_CONFIG"
wait

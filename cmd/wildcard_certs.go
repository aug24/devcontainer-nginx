package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

const (
	defaultWritableCertDir = "/tmp/devcontainer-nginx-certs"
	localMkcertRootDir    = "/etc/nginx/mkcert"
	localMkcertRootCert   = "/etc/nginx/mkcert/rootCA.pem"
	localMkcertRootKey    = "/etc/nginx/mkcert/rootCA-key.pem"
)

func defaultWildcardCertDir() string {
	candidate := strings.TrimSpace(os.Getenv("DEVCONTAINER_NGINX_CERT_DIR"))
	if candidate == "" {
		candidate = defaultWritableCertDir
	}
	if err := os.MkdirAll(candidate, 0o755); err == nil {
		return candidate
	}
	return defaultWritableCertDir
}

func ensureRouteWildcardCerts(routes []nginx.Route, baseDomain string) []nginx.Route {
	if len(routes) == 0 {
		return routes
	}

	certDir := defaultWildcardCertDir()
	log.Printf("Wildcard-cert policy: baseDomain=%q certDir=%q", baseDomain, certDir)

	for i := range routes {
		route := &routes[i]
		hostname := strings.TrimSpace(route.Hostname)
		if hostname == "" {
			continue
		}

		scope := hostnameWildcardScope(hostname, baseDomain)
		if scope == "" {
			continue
		}

		certPath, keyPath := wildcardCertPaths(scope, certDir)
		if certPath == "" || keyPath == "" {
			continue
		}

		if !fileExists(certPath) || !fileExists(keyPath) {
			if minted, err := mintWildcardCertificate(scope, certPath, keyPath); err != nil {
				log.Printf("WARNING: failed to mint wildcard cert for %q (scope=%q cert=%s key=%s): %v", hostname, scope, certPath, keyPath, err)
				continue
			} else if minted {
				log.Printf("MINTED wildcard certificate for %q: scope=%q cert=%s key=%s", hostname, scope, certPath, keyPath)
			}
		}

		route.CertPath = certPath
		route.KeyPath = keyPath
		log.Printf("Route %q is bound to wildcard cert scope %q -> %s", hostname, scope, certPath)
	}

	return routes
}

func hostnameWildcardScope(hostname, baseDomain string) string {
	hostname = strings.TrimSpace(strings.ToLower(hostname))
	baseDomain = strings.TrimSpace(strings.ToLower(baseDomain))
	if hostname == "" || baseDomain == "" {
		return ""
	}
	if !strings.HasSuffix(hostname, "."+baseDomain) {
		return ""
	}

	suffix := strings.TrimSuffix(hostname, "."+baseDomain)
	suffix = strings.Trim(suffix, ".")
	if suffix == "" {
		return baseDomain
	}

	parts := strings.Split(suffix, ".")
	if len(parts) <= 1 {
		return baseDomain
	}

	return strings.Join(parts[1:], ".") + "." + baseDomain
}

func wildcardCertPaths(scope, certDir string) (string, string) {
	scope = strings.TrimSpace(strings.ToLower(scope))
	if scope == "" || certDir == "" {
		return "", ""
	}

	safeScope := sanitizeDNSValue(scope)
	certPath := filepath.Join(certDir, fmt.Sprintf("wildcard.%s.crt", safeScope))
	keyPath := filepath.Join(certDir, fmt.Sprintf("wildcard.%s.key", safeScope))
	return certPath, keyPath
}

func sanitizeDNSValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "*", "")
	value = strings.ReplaceAll(value, ".", "_")
	value = strings.ReplaceAll(value, "-", "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "wildcard"
	}
	return value
}

func mintWildcardCertificate(scope, certPath, keyPath string) (bool, error) {
	if !fileExists(localMkcertRootCert) || !fileExists(localMkcertRootKey) {
		return false, fmt.Errorf("mkcert root CA not found at %s and %s", localMkcertRootCert, localMkcertRootKey)
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		return false, fmt.Errorf("failed to create cert directory %s: %w", filepath.Dir(certPath), err)
	}

	cmd := exec.Command("mkcert", "-cert-file", certPath, "-key-file", keyPath, "*."+scope, scope)
	cmd.Env = append(os.Environ(), "CAROOT="+filepath.Dir(localMkcertRootCert))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("mkcert failed for scope %q: %w: %s", scope, err, strings.TrimSpace(string(output)))
	}
	if !fileExists(certPath) || !fileExists(keyPath) {
		return false, fmt.Errorf("mkcert reported success but files are missing for scope %q", scope)
	}
	return true, nil
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

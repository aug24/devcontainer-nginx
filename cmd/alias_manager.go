package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/dev-tools/devcontainer-nginx/internal/nginx"
)

var aliasHostnameLabelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

type AliasEntry struct {
	SourceHostname string
	AliasHostname  string
	TargetHost     string
	TargetPort     int
	Protocol       string
	VerifySSL      bool
	VerifyName     string
	CertPath       string
	KeyPath        string
}

type AliasManager struct {
	mu     sync.RWMutex
	items  []AliasEntry
}

func NewAliasManager() *AliasManager {
	return &AliasManager{}
}

func (m *AliasManager) Snapshot() []AliasEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AliasEntry, len(m.items))
	copy(out, m.items)
	return out
}

func (m *AliasManager) AddAlias(sourceHostname string, rawAlias string, baseDomain string, routes []nginx.Route) (AliasEntry, error) {
	sourceHostname = strings.TrimSpace(sourceHostname)
	rawAlias = strings.TrimSpace(rawAlias)
	baseDomain = strings.TrimSpace(strings.ToLower(baseDomain))
	if sourceHostname == "" {
		return AliasEntry{}, fmt.Errorf("choose a mapped domain first")
	}
	if rawAlias == "" {
		return AliasEntry{}, fmt.Errorf("alias is required")
	}
	if baseDomain == "" {
		return AliasEntry{}, fmt.Errorf("base domain is required")
	}

	aliasHostname, err := normalizeAliasHostname(rawAlias, baseDomain)
	if err != nil {
		return AliasEntry{}, err
	}
	if aliasHostname == sourceHostname {
		return AliasEntry{}, fmt.Errorf("alias must be different from the mapped hostname")
	}

	match := findRouteByHostname(routes, sourceHostname)
	if match == nil {
		return AliasEntry{}, fmt.Errorf("hostname %q is not currently mapped", sourceHostname)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.items {
		if item.SourceHostname == sourceHostname && item.AliasHostname == aliasHostname {
			return item, nil
		}
		if item.AliasHostname == aliasHostname {
			return AliasEntry{}, fmt.Errorf("alias %q already exists", aliasHostname)
		}
	}

	entry := AliasEntry{
		SourceHostname: sourceHostname,
		AliasHostname:  aliasHostname,
		TargetHost:     match.TargetHost,
		TargetPort:     match.TargetPort,
		Protocol:       match.Protocol,
		VerifySSL:      match.VerifySSL,
		VerifyName:     match.VerifyName,
		CertPath:       match.CertPath,
		KeyPath:        match.KeyPath,
	}
	m.items = append(m.items, entry)
	return entry, nil
}

func (m *AliasManager) MergeRoutes(routes []nginx.Route) []nginx.Route {
	out := append([]nginx.Route(nil), routes...)
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, item := range m.items {
		source := findRouteByHostname(routes, item.SourceHostname)
		if source == nil {
			continue
		}
		if findRouteByHostname(out, item.AliasHostname) != nil {
			continue
		}
		aliasRoute := *source
		aliasRoute.Hostname = item.AliasHostname
		aliasRoute.CertPath = item.CertPath
		aliasRoute.KeyPath = item.KeyPath
		if aliasRoute.CertPath == "" {
			aliasRoute.CertPath = source.CertPath
		}
		if aliasRoute.KeyPath == "" {
			aliasRoute.KeyPath = source.KeyPath
		}
		out = append(out, aliasRoute)
	}
	return out
}

func (m *AliasManager) RemoveAlias(aliasHostname string) bool {
	aliasHostname = strings.TrimSpace(strings.ToLower(aliasHostname))
	if aliasHostname == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	filtered := m.items[:0]
	changed := false
	for _, item := range m.items {
		if strings.EqualFold(item.AliasHostname, aliasHostname) {
			changed = true
			continue
		}
		filtered = append(filtered, item)
	}
	m.items = filtered
	return changed
}

func normalizeAliasHostname(rawAlias string, baseDomain string) (string, error) {
	alias := strings.TrimSpace(strings.ToLower(rawAlias))
	alias = strings.TrimPrefix(alias, "https://")
	alias = strings.TrimPrefix(alias, "http://")
	alias = strings.Trim(alias, ".")
	if alias == "" {
		return "", fmt.Errorf("alias is required")
	}
	if strings.HasSuffix(alias, "."+baseDomain) {
		full := alias
		if err := validateHostnameUnderBaseDomain(full, baseDomain); err != nil {
			return "", err
		}
		return full, nil
	}
	full := alias + "." + baseDomain
	if err := validateHostnameUnderBaseDomain(full, baseDomain); err != nil {
		return "", fmt.Errorf("alias must be within the %s namespace: %w", baseDomain, err)
	}
	return full, nil
}

func validateHostnameUnderBaseDomain(hostname string, baseDomain string) error {
	hostname = strings.TrimSpace(strings.ToLower(hostname))
	baseDomain = strings.TrimSpace(strings.ToLower(baseDomain))
	if hostname == "" || baseDomain == "" {
		return fmt.Errorf("host is empty")
	}
	if hostname == baseDomain {
		return fmt.Errorf("host must not equal the base domain")
	}
	if !strings.HasSuffix(hostname, "."+baseDomain) {
		return fmt.Errorf("host %q is outside the %q namespace", hostname, baseDomain)
	}
	labelText := strings.TrimSuffix(hostname, "."+baseDomain)
	if labelText == "" {
		return fmt.Errorf("host must include a label before %q", baseDomain)
	}
	labels := strings.Split(labelText, ".")
	for _, label := range labels {
		if label == "" {
			return fmt.Errorf("host contains an empty label")
		}
		if len(label) > 63 || !aliasHostnameLabelRE.MatchString(label) {
			return fmt.Errorf("host label %q is not valid", label)
		}
	}
	return nil
}

func findRouteByHostname(routes []nginx.Route, hostname string) *nginx.Route {
	hostname = strings.TrimSpace(hostname)
	for i := range routes {
		if routes[i].Hostname == hostname {
			return &routes[i]
		}
	}
	return nil
}

func routeHostnames(routes []nginx.Route) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.Hostname == "" || seen[route.Hostname] {
			continue
		}
		seen[route.Hostname] = true
		out = append(out, route.Hostname)
	}
	sort.Strings(out)
	return out
}

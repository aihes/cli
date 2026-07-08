// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/transport"
)

const (
	remoteScopesPath    = "/lark-cli/apis/scopes.json"
	remoteScopesTimeout = 1 * time.Second
	maxRemoteScopesSize = 10 * 1024 * 1024 // 10MB, aligned with internal/registry/remote.go
)

// remoteScopesURLForTest is the injection seam for unit tests to point at an
// httptest server. It is empty at runtime, where the brand-hardcoded production
// URL is used, and must never carry an internal / non-production domain.
var remoteScopesURLForTest string

type remoteScopesFile struct {
	Scopes map[string]remoteDomainScopes `json:"scopes"`
}

type remoteDomainScopes struct {
	UserScopes   []string `json:"user_scopes"`
	TenantScopes []string `json:"tenant_scopes"`
}

func remoteScopesURL(brand core.LarkBrand) string {
	if remoteScopesURLForTest != "" {
		return remoteScopesURLForTest
	}
	return core.ResolveOpenBaseURL(brand) + remoteScopesPath
}

// FetchRemoteScopes fetches and binary-validates the remote scopes.json.
// It returns (domain -> user_scopes, true) when the whole file is usable, or
// (nil, false) when the caller should fall back to the local set. Any failure
// (network / timeout / non-2xx / empty / bad JSON / structure mismatch /
// malformed scope) returns (nil, false) silently — no warning, no telemetry.
func FetchRemoteScopes(brand core.LarkBrand) (map[string][]string, bool) {
	client := transport.NewHTTPClient(remoteScopesTimeout)
	req, err := http.NewRequest(http.MethodGet, remoteScopesURL(brand), nil)
	if err != nil {
		return nil, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteScopesSize))
	if err != nil || len(body) == 0 {
		return nil, false
	}
	var file remoteScopesFile
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, false
	}
	return validateRemoteScopes(file)
}

func validateRemoteScopes(file remoteScopesFile) (map[string][]string, bool) {
	if len(file.Scopes) == 0 {
		return nil, false
	}
	result := make(map[string][]string, len(file.Scopes))
	for domain, ds := range file.Scopes {
		if ds.UserScopes == nil { // missing user_scopes field / null → whole file untrusted
			return nil, false
		}
		for _, s := range ds.UserScopes {
			if !isValidScopeFormat(s) {
				return nil, false
			}
		}
		result[domain] = ds.UserScopes
	}
	return result, true
}

// isValidScopeFormat checks the service:resource:action shape: exactly three
// ":"-separated segments, each non-empty. The resource segment may contain "."
// (e.g. vc:meeting.meetingevent:read). i18n / tenant / version are not checked.
func isValidScopeFormat(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	return true
}

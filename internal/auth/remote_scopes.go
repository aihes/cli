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
	maxRemoteScopesSize = 10 * 1024 * 1024 // 10MB，对齐 internal/registry/remote.go
)

// remoteScopesURLForTest 是单元测试注入 httptest server 的 seam；
// 生产运行时为空，走 brand 硬编码生产地址。禁止用于承载任何内网 / 非生产域名的持久配置。
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

// FetchRemoteScopes 拉取并二值校验远端 scopes.json。
// 返回 (业务域 -> user_scopes, true) 表示整份可用；(nil, false) 表示应回退本地。
// 任何失败（网络/超时/非2xx/空/坏JSON/结构不符/畸形scope）均静默返回 (nil, false)，
// 不打印告警、不埋点。
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
		if ds.UserScopes == nil { // 缺 user_scopes 字段 / null → 整份不可信
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

// isValidScopeFormat 判定 service:resource:action 形态：按 ":" 恰好 3 段、每段非空。
// resource 段允许 "." （如 vc:meeting.meetingevent:read）。i18n/tenant/version 不校验。
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

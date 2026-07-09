// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/larksuite/cli/internal/core"
)

// AppRegistrationResponse is the response from the app registration begin endpoint.
type AppRegistrationResponse struct {
	DeviceCode              string
	UserCode                string
	VerificationUri         string
	VerificationUriComplete string
	ExpiresIn               int
	Interval                int
}

// AppRegistrationResult is the result of a successful app registration poll.
type AppRegistrationResult struct {
	ClientID     string
	ClientSecret string
	UserInfo     *AppRegUserInfo
	// AuthMethods is the authoritative auth method(s) the app must use, as
	// decided by the user/admin at confirmation (20260409 `auth_method` field).
	// It may differ from what the client requested — e.g. selecting an existing
	// client_secret app. Empty on older servers.
	AuthMethods []string
}

// AppRegUserInfo contains user info returned from app registration.
type AppRegUserInfo struct {
	OpenID      string
	TenantBrand string // "feishu" or "lark"
}

// AppRegistrationInit is the response from the app registration init endpoint.
type AppRegistrationInit struct {
	Nonce                string
	SupportedAuthMethods []string // e.g. ["client_secret", "private_key_jwt"]
}

// AppRegistrationBeginOptions parametrizes the registration begin request.
// A zero value selects the legacy client_secret flow, preserving prior behavior.
type AppRegistrationBeginOptions struct {
	AuthMethod      string // "" => client_secret; core.AuthMethodPrivateKeyJWT
	AuthAttestation string // private_key_jwt: the TEE-signed attestation JWT
	RestoreAppID    string // when set, asks the server to re-register this existing app
}

// RequestAppRegistrationInit performs the init step of the registration flow,
// returning a server nonce (to be embedded in a TEE-signed attestation JWT) and
// the auth methods the server supports for this archetype.
func RequestAppRegistrationInit(httpClient *http.Client) (*AppRegistrationInit, error) {
	// Registration always begins against the feishu accounts host (mirrors begin).
	endpoint := core.ResolveEndpoints(core.BrandFeishu).Accounts + PathAppRegistration

	form := url.Values{}
	form.Set("action", "init")
	form.Set("archetype", "PersonalAgent")

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	logHTTPResponse(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("app registration init failed: read body: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("app registration init failed: HTTP %d – response not JSON", resp.StatusCode)
	}

	if _, hasError := data["error"]; resp.StatusCode >= 400 || hasError {
		msg := getStr(data, "error_description")
		if msg == "" {
			msg = getStr(data, "error")
		}
		if msg == "" {
			msg = "Unknown error"
		}
		return nil, fmt.Errorf("app registration init failed: %s", msg)
	}

	out := &AppRegistrationInit{Nonce: getStr(data, "nonce")}
	if methods, ok := data["supported_auth_methods"].([]interface{}); ok {
		for _, m := range methods {
			if s, ok := m.(string); ok {
				out.SupportedAuthMethods = append(out.SupportedAuthMethods, s)
			}
		}
	}
	if out.Nonce == "" {
		return nil, fmt.Errorf("app registration init failed: server returned no nonce")
	}
	return out, nil
}

// RequestAppRegistration initiates the app registration device flow (begin step).
func RequestAppRegistration(httpClient *http.Client, brand core.LarkBrand, opts AppRegistrationBeginOptions, errOut io.Writer) (*AppRegistrationResponse, error) {
	if errOut == nil {
		errOut = io.Discard
	}

	ep := core.ResolveEndpoints(brand)
	regEp := core.ResolveEndpoints(core.BrandFeishu) // registration begin always uses feishu
	endpoint := regEp.Accounts + PathAppRegistration

	authMethod := opts.AuthMethod
	if authMethod == "" {
		authMethod = core.AuthMethodClientSecret
	}

	form := url.Values{}
	form.Set("action", "begin")
	form.Set("archetype", "PersonalAgent")
	form.Set("auth_method", authMethod)
	form.Set("request_user_info", "open_id tenant_brand")
	if opts.AuthAttestation != "" {
		form.Set("auth_attestation", opts.AuthAttestation)
	}
	// Restore flow: carry the existing app id so the server re-registers it
	// rather than creating a new app.
	if opts.RestoreAppID != "" {
		form.Set("app_id", opts.RestoreAppID)
	}

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	logHTTPResponse(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("app registration failed: read body: %v", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("app registration failed: HTTP %d – response not JSON", resp.StatusCode)
	}

	_, hasError := data["error"]
	if resp.StatusCode >= 400 || hasError {
		msg := getStr(data, "error_description")
		if msg == "" {
			msg = getStr(data, "error")
		}
		if msg == "" {
			msg = "Unknown error"
		}
		return nil, fmt.Errorf("app registration failed: %s", msg)
	}

	expiresIn := getInt(data, "expires_in", 300)
	interval := getInt(data, "interval", 5)

	userCode := getStr(data, "user_code")
	verificationUri := getStr(data, "verification_uri")
	// Prefer the server-provided complete URL (currently /page/launcher); fall
	// back to building it from verification_uri, then to /page/launcher. The old
	// hard-coded /page/cli is stale — the server now returns /page/launcher.
	verificationUriComplete := getStr(data, "verification_uri_complete")
	if verificationUriComplete == "" {
		base := verificationUri
		if base == "" {
			base = ep.Open + "/page/launcher"
		}
		// The server may return verification_uri with its own query (e.g.
		// app_id when registering against an existing app), so join with
		// the same ?/& logic as BuildVerificationURL.
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		verificationUriComplete = base + sep + "user_code=" + url.QueryEscape(userCode)
	}

	return &AppRegistrationResponse{
		DeviceCode:              getStr(data, "device_code"),
		UserCode:                getStr(data, "user_code"),
		VerificationUri:         verificationUri,
		VerificationUriComplete: verificationUriComplete,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

// parseAuthMethods normalizes the poll response `auth_method` field, which the
// server returns as a JSON array of strings (e.g. ["private_key_jwt"]) — or, on
// some variants, a single space-separated string.
func parseAuthMethods(v interface{}) []string {
	switch t := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, m := range t {
			if s, ok := m.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Fields(t)
	default:
		return nil
	}
}

// BuildVerificationURL appends CLI tracking parameters to the verification URL.
// When targetAppID is non-empty, it is also included so the launcher can lock
// authorization to that existing app.
func BuildVerificationURL(baseURL, cliVersion string, targetAppID ...string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return appendVerificationURLFallback(baseURL, cliVersion, targetAppID...)
	}
	q := u.Query()
	if q.Get("lpv") == "" {
		q.Set("lpv", cliVersion)
	}
	if q.Get("ocv") == "" {
		q.Set("ocv", cliVersion)
	}
	if q.Get("from") == "" {
		q.Set("from", "cli")
	}
	if len(targetAppID) > 0 && targetAppID[0] != "" && q.Get("app_id") == "" {
		q.Set("app_id", targetAppID[0])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func appendVerificationURLFallback(baseURL, cliVersion string, targetAppID ...string) string {
	sep := "&"
	if !strings.Contains(baseURL, "?") {
		sep = "?"
	}
	out := baseURL + sep + "lpv=" + url.QueryEscape(cliVersion) +
		"&ocv=" + url.QueryEscape(cliVersion) +
		"&from=cli"
	if len(targetAppID) > 0 && targetAppID[0] != "" && !strings.Contains(baseURL, "app_id=") {
		out += "&app_id=" + url.QueryEscape(targetAppID[0])
	}
	return out
}

// PollAppRegistration polls the app registration endpoint until the app is created or the flow times out.
// If the result has ClientSecret == "" and UserInfo.TenantBrand == "lark", the caller should
// retry with BrandLark to get the secret from accounts.larksuite.com.
func PollAppRegistration(ctx context.Context, httpClient *http.Client, brand core.LarkBrand, deviceCode string, interval, expiresIn int, errOut io.Writer) (*AppRegistrationResult, error) {
	if errOut == nil {
		errOut = io.Discard
	}

	const maxPollInterval = 60
	const maxPollAttempts = 200

	ep := core.ResolveEndpoints(brand)
	endpoint := ep.Accounts + PathAppRegistration
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	currentInterval := interval
	attempts := 0

	for time.Now().Before(deadline) && attempts < maxPollAttempts {
		attempts++
		if ctx.Err() != nil {
			return nil, fmt.Errorf("polling was cancelled")
		}

		select {
		case <-time.After(time.Duration(currentInterval) * time.Second):
		case <-ctx.Done():
			return nil, fmt.Errorf("polling was cancelled")
		}

		form := url.Values{}
		form.Set("action", "poll")
		form.Set("device_code", deviceCode)

		req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := httpClient.Do(req)
		if err != nil {
			fmt.Fprintf(errOut, "[lark-cli] [WARN] app-registration: poll network error: %v\n", err)
			currentInterval = minInt(currentInterval+1, maxPollInterval)
			continue
		}
		logHTTPResponse(resp)

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			fmt.Fprintf(errOut, "[lark-cli] [WARN] app-registration: poll read error: %v\n", err)
			currentInterval = minInt(currentInterval+1, maxPollInterval)
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(body, &data); err != nil {
			fmt.Fprintf(errOut, "[lark-cli] [WARN] app-registration: poll parse error: %v\n", err)
			currentInterval = minInt(currentInterval+1, maxPollInterval)
			continue
		}

		errStr := getStr(data, "error")

		// Success: app id present (server field is named client_id).
		if errStr == "" && getStr(data, "client_id") != "" {
			result := &AppRegistrationResult{
				ClientID:     getStr(data, "client_id"),
				ClientSecret: getStr(data, "client_secret"),
				AuthMethods:  parseAuthMethods(data["auth_method"]),
			}
			if userInfoRaw, ok := data["user_info"].(map[string]interface{}); ok {
				result.UserInfo = &AppRegUserInfo{
					OpenID:      getStr(userInfoRaw, "open_id"),
					TenantBrand: getStr(userInfoRaw, "tenant_brand"),
				}
			}
			return result, nil
		}

		switch errStr {
		case "authorization_pending":
			continue
		case "slow_down":
			currentInterval = minInt(currentInterval+5, maxPollInterval)
			fmt.Fprintf(errOut, "[lark-cli] app-registration: slow_down, interval increased to %ds\n", currentInterval)
			continue
		case "access_denied":
			return nil, fmt.Errorf("app registration denied by user")
		case "expired_token", "invalid_grant":
			return nil, fmt.Errorf("device code expired, please try again")
		}

		desc := getStr(data, "error_description")
		if desc == "" {
			desc = errStr
		}
		if desc == "" {
			desc = "Unknown error"
		}
		return nil, fmt.Errorf("app registration failed: %s", desc)
	}

	if attempts >= maxPollAttempts {
		fmt.Fprintf(errOut, "[lark-cli] [WARN] app-registration: max poll attempts (%d) reached\n", maxPollAttempts)
	}
	return nil, fmt.Errorf("app registration timed out, please try again")
}

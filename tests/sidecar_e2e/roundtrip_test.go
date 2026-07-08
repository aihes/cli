// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

//go:build authsidecar

// Package sidecar_e2e proves the sidecar auth-proxy wire protocol end-to-end,
// offline and secret-free: a real fork binary (built with -tags authsidecar,
// exercising the REAL extension/transport/sidecar interceptor) signs a
// request with HMAC-SHA256 and routes it to an in-test sidecar, which
// verifies the signature using the REAL sidecar.Verify / sidecar.CanonicalRequest
// from github.com/larksuite/cli/sidecar, injects a synthetic token, and
// forwards to an in-test mock upstream.
//
// DEVIATION FROM THE ORIGINAL PLAN: the plan called for driving the real
// sidecar/server-demo binary (built with -tags authsidecar_demo) as the
// middle process. That is infeasible for an OFFLINE test, for three
// independent reasons, all verified in source:
//
//  1. sidecar/server-demo/handler.go:171 resolves a REAL token via
//     h.cred.ResolveToken(...), which errors out unless the machine has run
//     `lark-cli auth login` — there is no way to make it return a token
//     without live credentials.
//  2. sidecar/server-demo/main.go builds handler.allowedHosts from
//     core.ResolveEndpoints(BrandFeishu/BrandLark) only — real feishu/lark
//     hosts. An in-test mock (127.0.0.1:<port>) is never in that allowlist
//     and would be rejected with 403 (handler.go step 4).
//  3. sidecar/server-demo/handler.go:184 pins the forward scheme to
//     "https://" + targetHost, ignoring the client-supplied scheme. It can
//     never be redirected to an http:// mock.
//
// server-demo's verify+inject logic is ALREADY covered by
// `go test -tags authsidecar_demo ./sidecar/server-demo/` (see the
// sidecar-test Makefile target, item 3) — that is unit-level coverage of the
// same code paths this file would otherwise exercise via a real subprocess.
//
// So instead, this test builds its OWN in-test sidecar (an httptest.Server)
// that mirrors server-demo/handler.go's verify+inject steps 0-8 exactly,
// using the real protocol package (sidecar.Verify, sidecar.CanonicalRequest,
// sidecar.BodySHA256, the Header* / Sentinel* / Identity* constants) — the
// same symbols server-demo itself uses. This is the standard shape for this
// kind of test: one real external process (the fork binary, compiled with
// the production interceptor code) plus two in-process httptest.Server
// stand-ins (sidecar, upstream). It proves the real wire protocol end-to-end
// without requiring live credentials, real feishu/lark hosts, or TLS.
//
// Every key/token/app-id here is an obviously-synthetic placeholder; nothing
// in this file can authenticate against anything real.
package sidecar_e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/larksuite/cli/sidecar"
)

// Synthetic, obviously-fake fixtures. None of these are real secrets.
const (
	testProxyKey  = "test-proxy-key-not-a-real-secret-000000000000"
	testAppID     = "cli_test_app_not_real"
	injectedToken = "fake-injected-token-not-real"
)

// capturedRequest snapshots the parts of an *http.Request that matter for
// assertions, taken before the request (and its body reader) is consumed or
// goes out of scope.
type capturedRequest struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

func snapshotRequest(r *http.Request, body []byte) capturedRequest {
	return capturedRequest{
		method:  r.Method,
		path:    r.URL.RequestURI(),
		headers: r.Header.Clone(),
		body:    body,
	}
}

// isProxyHeader reports whether name is one of the sidecar wire-protocol
// headers that must not be copied through to the forwarded (mock upstream)
// request. Mirrors sidecar/server-demo/handler.go's isProxyHeader.
func isProxyHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case http.CanonicalHeaderKey(sidecar.HeaderProxyVersion),
		http.CanonicalHeaderKey(sidecar.HeaderProxyTarget),
		http.CanonicalHeaderKey(sidecar.HeaderProxyIdentity),
		http.CanonicalHeaderKey(sidecar.HeaderProxySignature),
		http.CanonicalHeaderKey(sidecar.HeaderProxyTimestamp),
		http.CanonicalHeaderKey(sidecar.HeaderBodySHA256),
		http.CanonicalHeaderKey(sidecar.HeaderProxyAuthHeader):
		return true
	}
	return false
}

// parseTargetHost validates X-Lark-Proxy-Target and returns its host.
// Mirrors sidecar/server-demo/handler.go's parseTarget: the header must be
// "https://<host>" with no path, query, fragment, or userinfo. Only the host
// is used, both as HMAC signing input and to record what the fork believed
// its real destination was — the actual forward in this test always goes to
// the in-test mock, never to this host.
func parseTargetHost(target string) (string, error) {
	u, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("scheme must be https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("missing host")
	}
	if u.User != nil {
		return "", fmt.Errorf("userinfo not allowed")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("path not allowed (got %q)", u.Path)
	}
	if u.RawQuery != "" {
		return "", fmt.Errorf("query not allowed")
	}
	if u.Fragment != "" {
		return "", fmt.Errorf("fragment not allowed")
	}
	return u.Host, nil
}

// repoRoot resolves the lark-cli module root from the test's working
// directory (which `go test` sets to the package dir, tests/sidecar_e2e).
func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestSidecarHMACRoundTrip(t *testing.T) {
	root := repoRoot(t)
	sidecarKey := []byte(testProxyKey)

	// --- in-test mock upstream: stands in for open.feishu.cn ---
	var mockMu sync.Mutex
	var mockReq *capturedRequest
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		snap := snapshotRequest(r, body)
		mockMu.Lock()
		mockReq = &snap
		mockMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"document":{"content":"mock content"}}}`))
	}))
	defer mock.Close()

	// --- in-test sidecar: mirrors server-demo/handler.go's verify+inject path ---
	var scMu sync.Mutex
	var scReq *capturedRequest
	var verifyErr error
	var verifyRan bool
	sc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		snap := snapshotRequest(r, body)
		scMu.Lock()
		scReq = &snap
		scMu.Unlock()

		// Step 0: protocol version.
		version := r.Header.Get(sidecar.HeaderProxyVersion)
		if version != sidecar.ProtocolV1 {
			http.Error(w, "unsupported "+sidecar.HeaderProxyVersion+": "+version, http.StatusBadRequest)
			return
		}

		// Step 1-2: timestamp + body SHA256.
		ts := r.Header.Get(sidecar.HeaderProxyTimestamp)
		claimedSHA := r.Header.Get(sidecar.HeaderBodySHA256)
		actualSHA := sidecar.BodySHA256(body)
		if claimedSHA == "" || claimedSHA != actualSHA {
			http.Error(w, "body SHA256 mismatch", http.StatusBadRequest)
			return
		}

		// Step 3: target host, identity, auth-header (all covered by the sig).
		target := r.Header.Get(sidecar.HeaderProxyTarget)
		targetHost, perr := parseTargetHost(target)
		if perr != nil {
			http.Error(w, "invalid "+sidecar.HeaderProxyTarget+": "+perr.Error(), http.StatusForbidden)
			return
		}
		identity := r.Header.Get(sidecar.HeaderProxyIdentity)
		authHeader := r.Header.Get(sidecar.HeaderProxyAuthHeader)

		// Step 4: verify HMAC signature over the canonical request.
		signature := r.Header.Get(sidecar.HeaderProxySignature)
		err := sidecar.Verify(sidecarKey, sidecar.CanonicalRequest{
			Version:      version,
			Method:       r.Method,
			Host:         targetHost,
			PathAndQuery: r.URL.RequestURI(),
			BodySHA256:   claimedSHA,
			Timestamp:    ts,
			Identity:     identity,
			AuthHeader:   authHeader,
		}, signature)
		scMu.Lock()
		verifyErr = err
		verifyRan = true
		scMu.Unlock()
		if err != nil {
			http.Error(w, "HMAC verification failed: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Build the forward request. Unlike server-demo (which forwards to
		// "https://"+targetHost), this test forwards to the in-test MOCK's
		// URL — proving the sidecar's inject step without needing a real
		// upstream or a route to targetHost.
		forwardURL := mock.URL + r.URL.RequestURI()
		freq, ferr := http.NewRequest(r.Method, forwardURL, bytes.NewReader(body))
		if ferr != nil {
			http.Error(w, "failed to build forward request", http.StatusInternalServerError)
			return
		}
		for k, vs := range r.Header {
			if isProxyHeader(k) {
				continue
			}
			for _, v := range vs {
				freq.Header.Add(k, v)
			}
		}
		// Strip any client-supplied auth headers before injecting (mirrors
		// handler.go: the sidecar is the sole source of auth material).
		freq.Header.Del("Authorization")
		freq.Header.Del(sidecar.HeaderMCPUAT)
		freq.Header.Del(sidecar.HeaderMCPTAT)

		// Inject the synthetic token into the header the client committed to.
		if authHeader == "Authorization" {
			freq.Header.Set("Authorization", "Bearer "+injectedToken)
		} else {
			freq.Header.Set(authHeader, injectedToken)
		}

		resp, derr := http.DefaultClient.Do(freq)
		if derr != nil {
			http.Error(w, "forward failed: "+derr.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		for k, vs := range resp.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBody)
	}))
	defer sc.Close()

	scURL, err := url.Parse(sc.URL)
	if err != nil {
		t.Fatalf("parse sidecar URL: %v", err)
	}

	// --- build the fork: the REAL lark-cli built with -tags authsidecar ---
	binPath := filepath.Join(t.TempDir(), "forkbin")
	build := exec.Command("go", "build", "-tags", "authsidecar", "-o", binPath, ".")
	build.Dir = root
	if out, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build fork binary: %v\n%s", buildErr, out)
	}

	// --- run the fork against the in-test sidecar, fully offline ---
	configDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "docs", "+fetch", "--doc", "nonexistent", "--as", "user")
	cmd.Env = append(os.Environ(),
		"LARKSUITE_CLI_AUTH_PROXY=http://"+scURL.Host,
		"LARKSUITE_CLI_PROXY_KEY="+testProxyKey,
		"LARKSUITE_CLI_APP_ID="+testAppID,
		"LARKSUITE_CLI_BRAND=feishu",
		"LARKSUITE_CLI_CONFIG_DIR="+configDir,
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	t.Logf("fork exit error (informational only, not asserted): %v", runErr)
	t.Logf("fork stdout: %s", stdout.String())
	t.Logf("fork stderr: %s", stderr.String())

	// --- assertion (a): the sidecar received a request and its HMAC verified ---
	scMu.Lock()
	gotSC := scReq
	gotVerifyRan := verifyRan
	gotVerifyErr := verifyErr
	scMu.Unlock()

	if gotSC == nil {
		t.Fatal("sidecar never received a request from the fork — interceptor did not route to AUTH_PROXY")
	}
	if !gotVerifyRan {
		t.Fatal("sidecar received a request but never reached HMAC verification (rejected earlier — see handler headers)")
	}
	if gotVerifyErr != nil {
		t.Fatalf("HMAC verification failed on the fork's own signed request: %v", gotVerifyErr)
	}
	t.Logf("fork->sidecar headers: %v", gotSC.headers)

	// --- assertion (c), fork->sidecar half: no real Authorization ever left
	// the fork. The interceptor strips the sentinel before signing, so the
	// hop the sidecar actually sees must carry no Authorization header (and
	// no MCP auth header) at all.
	if auth := gotSC.headers.Get("Authorization"); auth != "" {
		t.Fatalf("fork->sidecar hop leaked an Authorization header (want none, interceptor should have stripped it): %q", auth)
	}
	if v := gotSC.headers.Get(sidecar.HeaderMCPUAT); v != "" {
		t.Fatalf("fork->sidecar hop leaked %s (want none): %q", sidecar.HeaderMCPUAT, v)
	}
	if v := gotSC.headers.Get(sidecar.HeaderMCPTAT); v != "" {
		t.Fatalf("fork->sidecar hop leaked %s (want none): %q", sidecar.HeaderMCPTAT, v)
	}
	// Proxy headers must be present (proves the interceptor actually ran).
	for _, h := range []string{
		sidecar.HeaderProxyVersion, sidecar.HeaderProxyTarget, sidecar.HeaderProxyIdentity,
		sidecar.HeaderProxySignature, sidecar.HeaderProxyTimestamp, sidecar.HeaderBodySHA256,
		sidecar.HeaderProxyAuthHeader,
	} {
		if gotSC.headers.Get(h) == "" {
			t.Fatalf("fork->sidecar hop missing required proxy header %s", h)
		}
	}
	if gotSC.headers.Get(sidecar.HeaderProxyIdentity) != sidecar.IdentityUser {
		t.Fatalf("fork->sidecar identity = %q, want %q", gotSC.headers.Get(sidecar.HeaderProxyIdentity), sidecar.IdentityUser)
	}

	// --- assertion (b): the mock upstream received the sidecar-injected
	// synthetic token, proving injection actually happened.
	mockMu.Lock()
	gotMock := mockReq
	mockMu.Unlock()

	if gotMock == nil {
		t.Fatal("mock upstream never received a forwarded request — sidecar did not forward after verification")
	}
	t.Logf("sidecar->mock headers: %v", gotMock.headers)

	wantAuth := "Bearer " + injectedToken
	gotAuth := gotMock.headers.Get("Authorization")
	if gotAuth != wantAuth {
		t.Fatalf("mock upstream Authorization = %q, want %q", gotAuth, wantAuth)
	}
	// Belt-and-suspenders: the value the mock saw must not be either sentinel
	// — proving the only token that ever reached "upstream" was the
	// sidecar-injected synthetic one, never a sentinel or a real token.
	if gotAuth == "Bearer "+sidecar.SentinelUAT || gotAuth == "Bearer "+sidecar.SentinelTAT {
		t.Fatalf("mock upstream received a sentinel token instead of the injected one: %q", gotAuth)
	}
}

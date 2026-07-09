// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/core"
	"github.com/smartystreets/goconvey/convey"
)

// Test_BuildVerificationURL verifies that tracking parameters are correctly appended.
func Test_BuildVerificationURL(t *testing.T) {
	t.Run("URL不含问号则添加?分隔符", func(t *testing.T) {
		result := BuildVerificationURL("https://example.com/verify", "1.0.0")
		got, err := url.Parse(result)
		if err != nil {
			t.Fatal(err)
		}
		convey.Convey("should add ? separator", t, func() {
			convey.So(got.Query().Get("lpv"), convey.ShouldEqual, "1.0.0")
			convey.So(got.Query().Get("ocv"), convey.ShouldEqual, "1.0.0")
			convey.So(got.Query().Get("from"), convey.ShouldEqual, "cli")
			convey.So(result, convey.ShouldStartWith, "https://example.com/verify?")
		})
	})

	t.Run("URL已含问号则添加&分隔符", func(t *testing.T) {
		result := BuildVerificationURL("https://example.com/verify?code=abc", "2.0.0")
		convey.Convey("should add & separator", t, func() {
			convey.So(result, convey.ShouldContainSubstring, "&lpv=2.0.0")
			convey.So(result, convey.ShouldContainSubstring, "&ocv=2.0.0")
			convey.So(result, convey.ShouldContainSubstring, "&from=cli")
			convey.So(result, convey.ShouldNotContainSubstring, "?lpv=")
		})
	})

	t.Run("指定已有应用时添加app_id", func(t *testing.T) {
		result := BuildVerificationURL("https://example.com/verify?user_code=abc", "2.0.0", "cli_existing")
		got, err := url.Parse(result)
		if err != nil {
			t.Fatal(err)
		}
		convey.Convey("should include target app_id", t, func() {
			convey.So(got.Query().Get("app_id"), convey.ShouldEqual, "cli_existing")
			convey.So(got.Query().Get("client_id"), convey.ShouldEqual, "")
			convey.So(got.Query().Get("lpv"), convey.ShouldEqual, "2.0.0")
		})
	})

	t.Run("服务端已返回app_id时不覆盖", func(t *testing.T) {
		result := BuildVerificationURL("https://example.com/verify?app_id=cli_server&user_code=abc", "2.0.0", "cli_existing")
		got, err := url.Parse(result)
		if err != nil {
			t.Fatal(err)
		}
		convey.Convey("should keep server app_id", t, func() {
			convey.So(got.Query().Get("app_id"), convey.ShouldEqual, "cli_server")
		})
	})
}

// captureClient returns an http.Client that records the last request's form body
// and replies with the given JSON payload.
func captureClient(gotBody *url.Values, respJSON string) *http.Client {
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Body != nil {
				b, _ := io.ReadAll(req.Body)
				v, _ := url.ParseQuery(string(b))
				*gotBody = v
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(respJSON)),
			}, nil
		}),
	}
}

func TestRequestAppRegistrationInit_ParsesNonceAndMethods(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, `{"nonce":"n-123","supported_auth_methods":["client_secret","private_key_jwt"]}`)

	out, err := RequestAppRegistrationInit(hc)
	if err != nil {
		t.Fatal(err)
	}
	if out.Nonce != "n-123" {
		t.Errorf("nonce = %q, want n-123", out.Nonce)
	}
	if len(out.SupportedAuthMethods) != 2 || out.SupportedAuthMethods[1] != "private_key_jwt" {
		t.Errorf("methods = %v", out.SupportedAuthMethods)
	}
	if body.Get("action") != "init" {
		t.Errorf("action = %q, want init", body.Get("action"))
	}
}

func TestRequestAppRegistrationInit_ErrorOnMissingNonce(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, `{"supported_auth_methods":["client_secret"]}`)
	if _, err := RequestAppRegistrationInit(hc); err == nil {
		t.Fatal("expected error when server returns no nonce")
	}
}

// TestRequestAppRegistrationInit_EmptySupportedAuthMethods covers the older-server
// back-compat path: an empty supported_auth_methods array parses to an empty
// slice, so the init guard in cmd/config/init_interactive.go
// (`len(SupportedAuthMethods) > 0 && !slices.Contains(...)`) stays false and does
// NOT reject the requested private_key_jwt. This aligns with
// resolveFinalAuthMethod(nil/[], private_key_jwt) == private_key_jwt
// (see cmd/config TestResolveFinalAuthMethod).
func TestRequestAppRegistrationInit_EmptySupportedAuthMethods(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, `{"nonce":"n-1","supported_auth_methods":[]}`)

	out, err := RequestAppRegistrationInit(hc)
	if err != nil {
		t.Fatal(err)
	}
	if out.Nonce != "n-1" {
		t.Errorf("nonce = %q, want n-1", out.Nonce)
	}
	if len(out.SupportedAuthMethods) != 0 {
		t.Errorf("SupportedAuthMethods = %v, want empty", out.SupportedAuthMethods)
	}
	// Reproduce the init guard expression on the real parsed result: an empty
	// slice must NOT reject private_key_jwt.
	rejected := len(out.SupportedAuthMethods) > 0 &&
		!slices.Contains(out.SupportedAuthMethods, core.AuthMethodPrivateKeyJWT)
	if rejected {
		t.Error("empty SupportedAuthMethods must allow private_key_jwt (older-server back-compat)")
	}
}

const beginRespJSON = `{"device_code":"dc","user_code":"uc","verification_uri":"https://example/verify","expires_in":300,"interval":5}`

func TestRequestAppRegistration_BeginDefaultsToClientSecret(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, beginRespJSON)

	if _, err := RequestAppRegistration(hc, core.BrandFeishu, AppRegistrationBeginOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if body.Get("action") != "begin" {
		t.Errorf("action = %q", body.Get("action"))
	}
	if body.Get("auth_method") != "client_secret" {
		t.Errorf("auth_method = %q, want client_secret (default)", body.Get("auth_method"))
	}
	if body.Has("auth_attestation") {
		t.Errorf("auth_attestation should be absent for client_secret, got %q", body.Get("auth_attestation"))
	}
	// Normal (non-restore) begin must NOT carry app_id.
	if body.Has("app_id") {
		t.Errorf("app_id should be absent when RestoreAppID is empty, got %q", body.Get("app_id"))
	}
}

// TestRequestAppRegistration_BeginRestoreAppID verifies the restore flow sends the
// existing app id on begin so the server re-registers that app.
func TestRequestAppRegistration_BeginRestoreAppID(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, beginRespJSON)

	opts := AppRegistrationBeginOptions{RestoreAppID: "cli_restore_me"}
	if _, err := RequestAppRegistration(hc, core.BrandFeishu, opts, nil); err != nil {
		t.Fatal(err)
	}
	if body.Get("action") != "begin" {
		t.Errorf("action = %q, want begin", body.Get("action"))
	}
	if body.Get("app_id") != "cli_restore_me" {
		t.Errorf("app_id = %q, want cli_restore_me", body.Get("app_id"))
	}
}

func TestRequestAppRegistration_VerificationURICompleteFallback(t *testing.T) {
	cases := []struct {
		name string
		resp string
		want string
	}{
		{
			name: "bare verification_uri",
			resp: `{"device_code":"dc","user_code":"uc","verification_uri":"https://example/verify","expires_in":300,"interval":5}`,
			want: "https://example/verify?user_code=uc",
		},
		{
			name: "verification_uri with existing query",
			resp: `{"device_code":"dc","user_code":"uc","verification_uri":"https://example/verify?app_id=cli_x","expires_in":300,"interval":5}`,
			want: "https://example/verify?app_id=cli_x&user_code=uc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body url.Values
			hc := captureClient(&body, tc.resp)
			got, err := RequestAppRegistration(hc, core.BrandFeishu, AppRegistrationBeginOptions{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.VerificationUriComplete != tc.want {
				t.Errorf("VerificationUriComplete = %q, want %q", got.VerificationUriComplete, tc.want)
			}
		})
	}
}

func TestParseAuthMethods(t *testing.T) {
	if got := parseAuthMethods([]interface{}{"private_key_jwt", "client_secret"}); len(got) != 2 || got[0] != "private_key_jwt" {
		t.Errorf("array form = %v", got)
	}
	if got := parseAuthMethods("client_secret private_key_jwt"); len(got) != 2 || got[1] != "private_key_jwt" {
		t.Errorf("string form = %v", got)
	}
	if got := parseAuthMethods(nil); got != nil {
		t.Errorf("nil form = %v, want nil", got)
	}
}

func TestRequestAppRegistration_BeginPrivateKeyJWT(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, beginRespJSON)

	opts := AppRegistrationBeginOptions{
		AuthMethod:      core.AuthMethodPrivateKeyJWT,
		AuthAttestation: "header.claims.sig",
	}
	if _, err := RequestAppRegistration(hc, core.BrandFeishu, opts, nil); err != nil {
		t.Fatal(err)
	}
	if body.Get("auth_method") != "private_key_jwt" {
		t.Errorf("auth_method = %q, want private_key_jwt", body.Get("auth_method"))
	}
	if body.Get("auth_attestation") != "header.claims.sig" {
		t.Errorf("auth_attestation = %q", body.Get("auth_attestation"))
	}
}

func TestRequestAppRegistration_BeginPrivateKeyJWTExistingAppID(t *testing.T) {
	var body url.Values
	hc := captureClient(&body, beginRespJSON)

	opts := AppRegistrationBeginOptions{
		AuthMethod:      core.AuthMethodPrivateKeyJWT,
		AuthAttestation: "header.claims.sig",
		RestoreAppID:    "cli_existing",
	}
	if _, err := RequestAppRegistration(hc, core.BrandFeishu, opts, nil); err != nil {
		t.Fatal(err)
	}
	if body.Get("auth_method") != "private_key_jwt" {
		t.Errorf("auth_method = %q, want private_key_jwt", body.Get("auth_method"))
	}
	if body.Get("auth_attestation") != "header.claims.sig" {
		t.Errorf("auth_attestation = %q", body.Get("auth_attestation"))
	}
	if body.Get("app_id") != "cli_existing" {
		t.Errorf("app_id = %q, want cli_existing", body.Get("app_id"))
	}
}

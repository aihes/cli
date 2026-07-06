// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package credential_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/larksuite/cli/errs"
	extcred "github.com/larksuite/cli/extension/credential"
	envprovider "github.com/larksuite/cli/extension/credential/env"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/credential"
	"github.com/larksuite/cli/internal/envvars"
	"github.com/larksuite/cli/internal/keychain"
)

func asConfigError(t *testing.T, err error) *errs.ConfigError {
	t.Helper()
	var ce *errs.ConfigError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *errs.ConfigError, got %T: %v", err, err)
	}
	return ce
}

func asValidationError(t *testing.T, err error) *errs.ValidationError {
	t.Helper()
	var ve *errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *errs.ValidationError, got %T: %v", err, err)
	}
	return ve
}

// secretValue is the profile secret written to config. It must NEVER appear in
// any error message or IdentitySelection (security §5.1).
const secretValue = "s3cr3t-tenant-a-value"

// envSecretValue is the direct env app secret. Same no-leak guarantee.
const envSecretValue = "env-secret-should-not-leak"

// writeConfigTenantA writes a config with a single profile "tenant_a" (app_id
// "cli_a"). The secret is a plaintext secret stored in config, which resolves
// locally without a keychain lookup.
func writeConfigTenantA(t *testing.T) {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	multi := &core.MultiAppConfig{
		CurrentApp: "tenant_a",
		Apps: []core.AppConfig{{
			Name:      "tenant_a",
			AppId:     "cli_a",
			AppSecret: core.PlainSecret(secretValue),
			Brand:     core.BrandFeishu,
		}},
	}
	if err := core.SaveMultiAppConfig(multi); err != nil {
		t.Fatalf("SaveMultiAppConfig: %v", err)
	}
}

// writeConfigTenantABroken writes tenant_a with a keychain-backed secret ref
// that cannot be resolved (noop keychain returns empty), so profile secret
// resolution fails locally.
func writeConfigTenantABroken(t *testing.T) {
	t.Helper()
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	// A keychain SecretRef whose key does NOT match app_id cli_a. Local secret
	// resolution fails (ValidateSecretKeyMatch), exercising profile_secret_invalid.
	multi := &core.MultiAppConfig{
		CurrentApp: "tenant_a",
		Apps: []core.AppConfig{{
			Name:      "tenant_a",
			AppId:     "cli_a",
			AppSecret: core.SecretInput{Ref: &core.SecretRef{Source: "keychain", ID: "appsecret:wrong_key"}},
			Brand:     core.BrandFeishu,
		}},
	}
	if err := core.SaveMultiAppConfig(multi); err != nil {
		t.Fatalf("SaveMultiAppConfig: %v", err)
	}
}

func newProvider(t *testing.T, profile string, fromFlag bool) *credential.CredentialProvider {
	t.Helper()
	ep := &envprovider.Provider{}
	defaultAcct := credential.NewDefaultAccountProvider(func() keychain.KeychainAccess { return &noopKC{} }, profile)
	cp := credential.NewCredentialProvider([]extcred.Provider{ep}, defaultAcct, nil, nil)
	cp.WithProfile(profile, fromFlag)
	return cp
}

// assertNoSecretLeak fails if any secret value appears in the given strings.
func assertNoSecretLeak(t *testing.T, where string, vals ...string) {
	t.Helper()
	for _, v := range vals {
		if v == "" {
			continue
		}
		if strings.Contains(v, secretValue) {
			t.Errorf("%s leaked profile secret: %q", where, v)
		}
		if strings.Contains(v, envSecretValue) {
			t.Errorf("%s leaked env secret: %q", where, v)
		}
	}
}

func subtypeOf(t *testing.T, err error) errs.Subtype {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	p, ok := errs.ProblemOf(err)
	if !ok {
		t.Fatalf("error is not a typed problem: %v", err)
	}
	return p.Subtype
}

// State #2: P none, E none, C none -> no_active_profile.
func TestSelection_State2_NoActiveProfile(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, "")
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir()) // empty dir -> no config
	cp := newProvider(t, "", false)

	sel, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeNoActiveProfile {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeNoActiveProfile)
	}
	assertNoSecretLeak(t, "state2", err.Error(), string(sel.Source))
}

// State #3: P none, E partial (only APP_ID) -> app_credential_incomplete.
func TestSelection_State3_EnvPartial(t *testing.T) {
	t.Setenv(envvars.CliAppID, "cli_env")
	t.Setenv(envvars.CliAppSecret, "")
	writeConfigTenantA(t)
	cp := newProvider(t, "", false)

	_, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeAppCredentialIncomplete {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeAppCredentialIncomplete)
	}
	prob, _ := errs.ProblemOf(err)
	ce := asConfigError(t, err)
	if !slices.Contains(ce.MissingKeys, envvars.CliAppSecret) {
		t.Errorf("missing_keys = %v, want to contain %q", ce.MissingKeys, envvars.CliAppSecret)
	}
	// missing_keys must be NAMES only, never values.
	for _, k := range ce.MissingKeys {
		if strings.Contains(k, envSecretValue) || strings.Contains(k, secretValue) {
			t.Errorf("missing_keys contains a value, not a name: %q", k)
		}
	}
	assertNoSecretLeak(t, "state3", prob.Message, prob.Hint)
}

// State #4: P none, E complete -> env:LARKSUITE_CLI_APP_ID.
func TestSelection_State4_EnvComplete(t *testing.T) {
	t.Setenv(envvars.CliAppID, "cli_env")
	t.Setenv(envvars.CliAppSecret, envSecretValue)
	writeConfigTenantA(t)
	cp := newProvider(t, "", false)

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Source != credential.SourceEnvAppID {
		t.Fatalf("source = %q, want %q", sel.Source, credential.SourceEnvAppID)
	}
	if !sel.DirectCredentialEnv.Present {
		t.Errorf("DirectCredentialEnv.Present = false, want true")
	}
	assertNoSecretLeak(t, "state4", string(sel.Source), sel.Suggestion, sel.DirectCredentialEnv.AppID)
	assertNoSecretLeak(t, "state4-keys", sel.DirectCredentialEnv.Keys...)
}

// State #5: P valid, E none -> flag:--profile (fromFlag) source.
func TestSelection_State5_ProfileOnly(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, "")
	writeConfigTenantA(t)
	cp := newProvider(t, "tenant_a", true)

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Source != credential.SourceFlagProfile {
		t.Fatalf("source = %q, want %q", sel.Source, credential.SourceFlagProfile)
	}
	if sel.DirectCredentialEnv.Present {
		t.Errorf("DirectCredentialEnv.Present = true, want false")
	}
	assertNoSecretLeak(t, "state5", string(sel.Source), sel.Suggestion)
}

// State #5b: P valid from env (not flag) -> env:LARKSUITE_CLI_PROFILE source.
func TestSelection_State5_ProfileFromEnv(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, "")
	writeConfigTenantA(t)
	cp := newProvider(t, "tenant_a", false)

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Source != credential.SourceEnvProfile {
		t.Fatalf("source = %q, want %q", sel.Source, credential.SourceEnvProfile)
	}
}

// State #6: P missing (nonexistent), E complete -> profile_not_found.
func TestSelection_State6_ProfileNotFound(t *testing.T) {
	t.Setenv(envvars.CliAppID, "cli_env")
	t.Setenv(envvars.CliAppSecret, envSecretValue)
	writeConfigTenantA(t)
	cp := newProvider(t, "does_not_exist", true)

	sel, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeProfileNotFound {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeProfileNotFound)
	}
	prob, _ := errs.ProblemOf(err)
	assertNoSecretLeak(t, "state6", err.Error(), prob.Hint, string(sel.Source))
}

// State #7: P valid but secret broken, E none -> profile_secret_invalid.
func TestSelection_State7_ProfileSecretInvalid(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, "")
	writeConfigTenantABroken(t)
	cp := newProvider(t, "tenant_a", true)

	_, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeProfileSecretInvalid {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeProfileSecretInvalid)
	}
	ce := asConfigError(t, err)
	if ce.Profile != "tenant_a" {
		t.Errorf("profile = %q, want tenant_a", ce.Profile)
	}
	if ce.AppID != "cli_a" {
		t.Errorf("app_id = %q, want cli_a", ce.AppID)
	}
	assertNoSecretLeak(t, "state7", ce.Message, ce.Hint)
}

// State #8: P valid, E complete, app_id matches -> profile source, env present+matched.
func TestSelection_State8_ProfileMatchesEnv(t *testing.T) {
	t.Setenv(envvars.CliAppID, "cli_a") // matches profile app_id
	t.Setenv(envvars.CliAppSecret, envSecretValue)
	writeConfigTenantA(t)
	cp := newProvider(t, "tenant_a", true)

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Source != credential.SourceFlagProfile {
		t.Fatalf("source = %q, want %q", sel.Source, credential.SourceFlagProfile)
	}
	if !sel.DirectCredentialEnv.Present || !sel.DirectCredentialEnv.Matched {
		t.Fatalf("DirectCredentialEnv = %+v, want Present && Matched", sel.DirectCredentialEnv)
	}
	if sel.DirectCredentialEnv.AppID != "cli_a" {
		t.Errorf("DirectCredentialEnv.AppID = %q, want cli_a", sel.DirectCredentialEnv.AppID)
	}
	assertNoSecretLeak(t, "state8", string(sel.Source), sel.Suggestion, sel.DirectCredentialEnv.AppID)
	assertNoSecretLeak(t, "state8-keys", sel.DirectCredentialEnv.Keys...)
}

// State #9: P valid, E complete, app_id mismatches -> profile_app_credential_conflict.
func TestSelection_State9_Conflict(t *testing.T) {
	t.Setenv(envvars.CliAppID, "cli_x") // mismatches profile app_id cli_a
	t.Setenv(envvars.CliAppSecret, envSecretValue)
	writeConfigTenantA(t)
	cp := newProvider(t, "tenant_a", true)

	_, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeProfileAppCredentialConflict {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeProfileAppCredentialConflict)
	}
	ve := asValidationError(t, err)
	if ve.ProfileAppID != "cli_a" {
		t.Errorf("profile_app_id = %q, want cli_a", ve.ProfileAppID)
	}
	if ve.EnvAppID != "cli_x" {
		t.Errorf("env_app_id = %q, want cli_x", ve.EnvAppID)
	}
	assertNoSecretLeak(t, "state9", ve.Message, ve.Hint)
}

// State #10: P valid, E partial -> app_credential_incomplete (env-partial wins).
func TestSelection_State10_ProfileWithEnvPartial(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, envSecretValue) // only secret set
	writeConfigTenantA(t)
	cp := newProvider(t, "tenant_a", true)

	_, err := cp.Selection(context.Background())
	if got := subtypeOf(t, err); got != errs.SubtypeAppCredentialIncomplete {
		t.Fatalf("subtype = %q, want %q", got, errs.SubtypeAppCredentialIncomplete)
	}
	ce := asConfigError(t, err)
	if !slices.Contains(ce.MissingKeys, envvars.CliAppID) {
		t.Errorf("missing_keys = %v, want to contain %q", ce.MissingKeys, envvars.CliAppID)
	}
	assertNoSecretLeak(t, "state10", ce.Message, ce.Hint)
	assertNoSecretLeak(t, "state10-keys", ce.MissingKeys...)
}

// fakeSidecarProvider is a NON-env extension provider (Priority 0, Name !=
// directCredentialProviderName) that always returns a non-nil account. It
// stands in for the sidecar extension provider without needing a build tag.
type fakeSidecarProvider struct {
	appID string
}

func (f *fakeSidecarProvider) Name() string  { return "sidecar" }
func (f *fakeSidecarProvider) Priority() int { return 0 }
func (f *fakeSidecarProvider) ResolveAccount(ctx context.Context) (*extcred.Account, error) {
	return &extcred.Account{AppID: f.appID, Brand: extcred.Brand("feishu")}, nil
}
func (f *fakeSidecarProvider) ResolveToken(ctx context.Context, req extcred.TokenSpec) (*extcred.Token, error) {
	return &extcred.Token{Value: "sidecar-tok", Source: "sidecar"}, nil
}

// Regression: a NON-env extension provider (sidecar) that returns an account
// must win outright even when a profile is set. It must NOT be treated as a
// direct-credential env account: no profile arbitration, no
// profile_app_credential_conflict (even though its app_id differs from the
// profile's cli_a), and DirectCredentialEnv.Present must stay false (§4.2 —
// no direct env vars are set). This proves the success-account provider gating
// mirrors the block-path guard.
func TestSelection_NonEnvExtensionProviderWinsOverProfile(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")     // no direct env credential
	t.Setenv(envvars.CliAppSecret, "") // no direct env credential
	writeConfigTenantA(t)              // profile tenant_a exists, app_id cli_a

	sidecar := &fakeSidecarProvider{appID: "sidecar_app"} // differs from cli_a
	defaultAcct := credential.NewDefaultAccountProvider(func() keychain.KeychainAccess { return &noopKC{} }, "tenant_a")
	cp := credential.NewCredentialProvider([]extcred.Provider{sidecar}, defaultAcct, nil, nil)
	cp.WithProfile("tenant_a", true)

	acct, err := cp.ResolveAccount(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The sidecar account is used as-is, NOT overridden by profile arbitration.
	if acct == nil || acct.AppID != "sidecar_app" {
		t.Fatalf("account = %+v, want AppID sidecar_app (sidecar wins outright)", acct)
	}

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected Selection error: %v", err)
	}
	// No misreported direct env credential (§4.2).
	if sel.DirectCredentialEnv.Present {
		t.Errorf("DirectCredentialEnv.Present = true, want false (no direct env vars set)")
	}
	// The mismatched app_id (sidecar_app vs profile cli_a) must NOT trigger a
	// profile_app_credential_conflict: both ResolveAccount and Selection above
	// returned nil errors, so no conflict (or any other) error was produced.
	// Guard against a future regression that surfaces a conflict via Selection.
	if _, selErr := cp.Selection(context.Background()); selErr != nil {
		if subtypeOf(t, selErr) == errs.SubtypeProfileAppCredentialConflict {
			t.Errorf("got profile_app_credential_conflict, want none for non-env provider")
		}
	}
	assertNoSecretLeak(t, "nonenv-sidecar", string(sel.Source), sel.Suggestion, sel.DirectCredentialEnv.AppID)
}

// State #1: P none, E none, C present -> config default (currentApp).
func TestSelection_State1_ConfigDefault(t *testing.T) {
	t.Setenv(envvars.CliAppID, "")
	t.Setenv(envvars.CliAppSecret, "")
	writeConfigTenantA(t) // CurrentApp = tenant_a
	cp := newProvider(t, "", false)

	sel, err := cp.Selection(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.Source != credential.SourceConfigCurrentApp {
		t.Fatalf("source = %q, want %q", sel.Source, credential.SourceConfigCurrentApp)
	}
}

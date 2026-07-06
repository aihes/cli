// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package credential

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/larksuite/cli/errs"
	extcred "github.com/larksuite/cli/extension/credential"
	"github.com/larksuite/cli/internal/auth"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/envvars"
)

// directCredentialProviderName is the Name() of the env provider, the source
// of direct app credentials (LARKSUITE_CLI_APP_ID / _APP_SECRET). Only its
// incomplete blocks map to app_credential_incomplete (spec §3 step 1).
const directCredentialProviderName = "env"

// DefaultAccountResolver is implemented by the default account provider.
type DefaultAccountResolver interface {
	ResolveAccount(ctx context.Context) (*Account, error)
}

// DefaultTokenResolver is implemented by the default token provider.
type DefaultTokenResolver interface {
	ResolveToken(ctx context.Context, req TokenSpec) (*TokenResult, error)
}

var (
	getStoredToken       = auth.GetStoredToken
	getStoredTokenStatus = auth.TokenStatus
)

type credentialSource interface {
	Name() string
	TryResolveToken(ctx context.Context, req TokenSpec) (*TokenResult, bool, error)
	ResolveIdentityHint(ctx context.Context, acct *Account) (*IdentityHint, error)
}

type extensionTokenSource struct {
	provider extcred.Provider
}

func (s extensionTokenSource) Name() string { return s.provider.Name() }

func (s extensionTokenSource) TryResolveToken(ctx context.Context, req TokenSpec) (*TokenResult, bool, error) {
	tok, err := s.provider.ResolveToken(ctx, extcred.TokenSpec{
		Type:  extcred.TokenType(req.Type.String()),
		AppID: req.AppID,
	})
	if err != nil {
		return nil, false, err
	}
	if tok == nil {
		return nil, false, nil
	}
	if tok.Value == "" {
		return nil, false, &MalformedTokenResultError{Source: s.Name(), Type: req.Type, Reason: "empty token"}
	}
	return &TokenResult{Token: tok.Value, Scopes: tok.Scopes}, true, nil
}

func (s extensionTokenSource) ResolveIdentityHint(ctx context.Context, acct *Account) (*IdentityHint, error) {
	hint := &IdentityHint{}
	if acct == nil {
		return hint, nil
	}
	hint.DefaultAs = acct.DefaultAs
	// Extension sources verify user identity via enrichUserInfo, so a resolved
	// UserOpenId is sufficient here; no keychain-backed token status lookup is needed.
	if acct.UserOpenId != "" {
		hint.AutoAs = core.AsUser
		return hint, nil
	}
	ids := extcred.IdentitySupport(acct.SupportedIdentities)
	switch {
	case ids.UserOnly():
		hint.AutoAs = core.AsUser
	case ids.BotOnly():
		hint.AutoAs = core.AsBot
	}
	return hint, nil
}

type defaultTokenSource struct {
	resolver DefaultTokenResolver
}

func (s defaultTokenSource) Name() string { return "default" }

func (s defaultTokenSource) TryResolveToken(ctx context.Context, req TokenSpec) (*TokenResult, bool, error) {
	if s.resolver == nil {
		return nil, false, nil
	}
	result, err := s.resolver.ResolveToken(ctx, req)
	if err != nil {
		return nil, false, err
	}
	if result == nil {
		return nil, false, &MalformedTokenResultError{Source: s.Name(), Type: req.Type, Reason: "nil token result"}
	}
	if result.Token == "" {
		return nil, false, &MalformedTokenResultError{Source: s.Name(), Type: req.Type, Reason: "empty token"}
	}
	return result, true, nil
}

func (s defaultTokenSource) ResolveIdentityHint(ctx context.Context, acct *Account) (*IdentityHint, error) {
	hint := &IdentityHint{}
	if acct == nil {
		return hint, nil
	}
	hint.DefaultAs = acct.DefaultAs
	if acct.UserOpenId == "" {
		hint.AutoAs = core.AsBot
		return hint, nil
	}
	stored := getStoredToken(acct.AppID, acct.UserOpenId)
	if stored == nil {
		hint.AutoAs = core.AsBot
		return hint, nil
	}
	if getStoredTokenStatus(stored) == "expired" {
		hint.AutoAs = core.AsBot
		return hint, nil
	}
	hint.AutoAs = core.AsUser
	return hint, nil
}

// CredentialProvider is the unified entry point for all credential resolution.
type CredentialProvider struct {
	providers    []extcred.Provider
	defaultAcct  DefaultAccountResolver
	defaultToken DefaultTokenResolver
	httpClient   func() (*http.Client, error)
	warnOut      io.Writer

	// profile is the active profile (from --profile or LARKSUITE_CLI_PROFILE).
	// profileFromFlag discriminates the source for the reported selection.
	profile         string
	profileFromFlag bool

	accountOnce    sync.Once
	account        *Account
	accountErr     error
	selectedSource credentialSource
	// selection is the explainable credential-selection result, populated by
	// doResolveAccount under accountOnce. It never carries a secret (§5.1).
	selection IdentitySelection

	hintOnce sync.Once
	hint     *IdentityHint
	hintErr  error
}

// NewCredentialProvider creates a CredentialProvider.
func NewCredentialProvider(providers []extcred.Provider, defaultAcct DefaultAccountResolver, defaultToken DefaultTokenResolver, httpClient func() (*http.Client, error)) *CredentialProvider {
	return &CredentialProvider{
		providers:    providers,
		defaultAcct:  defaultAcct,
		defaultToken: defaultToken,
		httpClient:   httpClient,
	}
}

func (p *CredentialProvider) SetWarnOut(warnOut io.Writer) *CredentialProvider {
	p.warnOut = warnOut
	return p
}

// WithProfile records the active profile and whether it came from the
// --profile flag (as opposed to the LARKSUITE_CLI_PROFILE env fallback).
// It governs credential arbitration and the reported selection source.
func (p *CredentialProvider) WithProfile(profile string, fromFlag bool) *CredentialProvider {
	p.profile = profile
	p.profileFromFlag = fromFlag
	return p
}

// ResolveAccount resolves app credentials. Result is cached after first call.
// NOTE: Uses sync.Once — only the context from the first call is used for resolution.
// Subsequent calls return the cached result regardless of their context.
// This is acceptable for CLI (single invocation per process) but not for long-running servers.
func (p *CredentialProvider) ResolveAccount(ctx context.Context) (*Account, error) {
	p.accountOnce.Do(func() {
		p.account, p.accountErr = p.doResolveAccount(ctx)
	})
	return p.account, p.accountErr
}

// doResolveAccount arbitrates the credential/App selection per the spec
// resolution order (§3): env-partial → profile → env-complete → config default.
// It populates p.selection (no secret; §5.1) and p.selectedSource on every
// success path.
func (p *CredentialProvider) doResolveAccount(ctx context.Context) (*Account, error) {
	// Step 1 (spec §3): consult the extension providers. The env provider is
	// the "direct app credential" source. An incomplete direct credential
	// (only APP_ID or only APP_SECRET set) short-circuits to
	// app_credential_incomplete regardless of the active profile.
	var envAcct *Account
	var envSource extensionTokenSource
	for _, prov := range p.providers {
		acct, err := prov.ResolveAccount(ctx)
		if err != nil {
			var blockErr *extcred.BlockError
			// Only the env (direct-credential) provider maps an incomplete
			// block to app_credential_incomplete. Other providers' blocks
			// propagate unchanged so they still stop the chain (§3 step 1
			// is specifically about direct app credential env vars).
			if errors.As(err, &blockErr) && prov.Name() == directCredentialProviderName {
				if missing := missingDirectCredentialKeys(); len(missing) > 0 {
					return nil, errs.NewConfigError(errs.SubtypeAppCredentialIncomplete,
						"direct app credential is incomplete").
						WithMissingKeys(missing...).
						WithHint("set both %s and %s, or unset both and use --profile / a config default.",
							envvars.CliAppID, envvars.CliAppSecret)
				}
				// Block for a reason other than incompleteness (e.g. an
				// invalid identity/strict-mode value); preserve prior behavior.
				return nil, err
			}
			return nil, err
		}
		if acct != nil {
			// Only the env (direct-credential) provider feeds profile
			// arbitration / conflict detection / DirectCredentialEnv reporting.
			// This mirrors the block-path guard above. A non-env extension
			// provider (e.g. sidecar) is NOT a direct-credential env account:
			// it wins outright here, returning its account + token source
			// unchanged (pre-diff behavior), without being misreported as a
			// direct env credential (§4.2: Present = direct env vars actually
			// set) or triggering a spurious profile_app_credential_conflict.
			if prov.Name() != directCredentialProviderName {
				internal := convertAccount(acct)
				source := extensionTokenSource{provider: prov}
				if err := p.enrichUserInfo(ctx, internal, source); err != nil {
					if p.warnOut != nil {
						_, _ = fmt.Fprintf(p.warnOut, "warning: unable to verify user identity from credential source %q: %v\n", source.Name(), err)
					}
					// enrichUserInfo failure is non-fatal: SupportedIdentities
					// (used for strict mode) is already set by the provider.
					// Clear unverified user identity for safety.
					internal.UserOpenId = ""
					internal.UserName = ""
				}
				p.selectedSource = source
				return internal, nil
			}
			envAcct = convertAccount(acct)
			envSource = extensionTokenSource{provider: prov}
			break
		}
	}

	// Step 2 (spec §3): an explicit profile was requested.
	if p.profile != "" {
		multi, loadErr := core.LoadMultiAppConfig()
		var app *core.AppConfig
		if loadErr == nil && multi != nil {
			app = multi.FindApp(p.profile)
		}
		if app == nil {
			return nil, errs.NewConfigError(errs.SubtypeProfileNotFound,
				"profile %q not found", p.profile).
				WithProfile(p.profile).
				WithHint("run `lark-cli profile list` to see available profiles.")
		}
		if envAcct != nil {
			// E == complete: the direct env app_id must match the profile.
			if app.AppId != envAcct.AppID {
				return nil, errs.NewValidationError(errs.SubtypeProfileAppCredentialConflict,
					"profile %q app_id does not match %s", p.profile, envvars.CliAppID).
					WithProfileAppConflict(app.AppId, envAcct.AppID).
					WithHint("unset %s/%s, or select a profile whose app_id matches the environment.",
						envvars.CliAppID, envvars.CliAppSecret)
			}
			p.selection = IdentitySelection{
				Source: p.profileSource(),
				DirectCredentialEnv: DirectCredentialEnv{
					Present: true,
					Keys:    presentDirectCredentialKeys(),
					AppID:   envAcct.AppID,
					Matched: true,
				},
			}
		} else {
			p.selection = IdentitySelection{
				Source:              p.profileSource(),
				DirectCredentialEnv: DirectCredentialEnv{Present: false},
			}
		}
		// Resolve the profile's own (keychain-backed) credential locally.
		acct, err := p.defaultAcct.ResolveAccount(ctx)
		if err != nil {
			// SECURITY (§5.1): generic message — never embed the underlying
			// error or any secret material.
			p.selection = IdentitySelection{}
			return nil, errs.NewConfigError(errs.SubtypeProfileSecretInvalid,
				"profile %q credential could not be resolved locally", p.profile).
				WithProfile(p.profile).
				WithAppID(app.AppId).
				WithHint("verify the profile's app secret or re-add the profile with `lark-cli config`.")
		}
		p.selection.Suggestion = "如需临时覆盖本条命令，使用 --profile。"
		p.selectedSource = defaultTokenSource{resolver: p.defaultToken}
		return acct, nil
	}

	// Step 3 (spec §3): no explicit profile — direct env credential wins.
	if envAcct != nil {
		if err := p.enrichUserInfo(ctx, envAcct, envSource); err != nil {
			if p.warnOut != nil {
				_, _ = fmt.Fprintf(p.warnOut, "warning: unable to verify user identity from credential source %q: %v\n", envSource.Name(), err)
			}
			// enrichUserInfo failure is non-fatal: SupportedIdentities
			// (used for strict mode) is already set by the provider.
			// Clear unverified user identity for safety.
			envAcct.UserOpenId = ""
			envAcct.UserName = ""
		}
		p.selectedSource = envSource
		p.selection = IdentitySelection{
			Source: SourceEnvAppID,
			DirectCredentialEnv: DirectCredentialEnv{
				Present: true,
				Keys:    presentDirectCredentialKeys(),
				AppID:   envAcct.AppID,
			},
		}
		return envAcct, nil
	}

	// No direct env credential and no profile → the config default.
	if p.defaultAcct != nil {
		acct, err := p.defaultAcct.ResolveAccount(ctx)
		if err != nil {
			// No usable default identity → no_active_profile. Other typed
			// failures (e.g. a specific config error) pass through unchanged.
			if prob, ok := errs.ProblemOf(err); ok && prob.Subtype == errs.SubtypeNotConfigured {
				return nil, errs.NewConfigError(errs.SubtypeNoActiveProfile, "no active profile").
					WithHint("run `lark-cli config init` / `lark-cli profile add`, or set %s.", envvars.CliProfile)
			}
			return nil, err
		}
		multi, _ := core.LoadMultiAppConfig()
		p.selectedSource = defaultTokenSource{resolver: p.defaultToken}
		p.selection = IdentitySelection{Source: selectionSourceForDefault(multi)}
		return acct, nil
	}
	return nil, core.NotConfiguredError()
}

// profileSource reports the credential source kind for a profile-backed
// selection, discriminating the --profile flag from the env fallback.
func (p *CredentialProvider) profileSource() CredentialSourceKind {
	if p.profileFromFlag {
		return SourceFlagProfile
	}
	return SourceEnvProfile
}

// selectionSourceForDefault reports whether the config default resolved to the
// explicit currentApp or fell back to the first app (spec §3 step 3.2).
func selectionSourceForDefault(multi *core.MultiAppConfig) CredentialSourceKind {
	if multi != nil && multi.CurrentApp != "" {
		return SourceConfigCurrentApp
	}
	return SourceConfigFirstApp
}

// missingDirectCredentialKeys returns the NAMES (never values) of the direct
// app credential env vars that are absent. Used only when the env provider
// blocks, to map an incomplete direct credential to app_credential_incomplete.
func missingDirectCredentialKeys() []string {
	var missing []string
	if os.Getenv(envvars.CliAppID) == "" {
		missing = append(missing, envvars.CliAppID)
	}
	if os.Getenv(envvars.CliAppSecret) == "" {
		missing = append(missing, envvars.CliAppSecret)
	}
	return missing
}

// presentDirectCredentialKeys returns the NAMES (never values) of the direct
// app credential env vars that are set. Used to annotate DirectCredentialEnv.
func presentDirectCredentialKeys() []string {
	var keys []string
	if os.Getenv(envvars.CliAppID) != "" {
		keys = append(keys, envvars.CliAppID)
	}
	if os.Getenv(envvars.CliAppSecret) != "" {
		keys = append(keys, envvars.CliAppSecret)
	}
	return keys
}

// Selection resolves the account (once) and returns the cached, secret-free
// explanation of how the credential/App was selected. It mirrors
// selectedCredentialSource: resolve-then-return.
func (p *CredentialProvider) Selection(ctx context.Context) (IdentitySelection, error) {
	if _, err := p.ResolveAccount(ctx); err != nil {
		return IdentitySelection{}, err
	}
	return p.selection, nil
}

// enrichUserInfo resolves user identity when extension provides a UAT.
// If UAT is available, user_info API call is mandatory (security: verify token validity).
// If no UAT from extension, falls back to provider-supplied OpenID.
func (p *CredentialProvider) enrichUserInfo(ctx context.Context, acct *Account, source credentialSource) error {
	if p.httpClient == nil || source == nil {
		return nil
	}
	tok, found, err := source.TryResolveToken(ctx, TokenSpec{Type: TokenTypeUAT, AppID: acct.AppID})
	if err != nil {
		var blockErr *extcred.BlockError
		if errors.As(err, &blockErr) {
			return nil // provider explicitly blocks UAT; skip enrichment
		}
		return fmt.Errorf("failed to resolve UAT for user identity verification: %w", err)
	}
	if !found {
		return nil
	}
	// Have UAT — must verify and resolve identity
	hc, err := p.httpClient()
	if err != nil {
		return fmt.Errorf("failed to get HTTP client for user_info: %w", err)
	}
	info, err := fetchUserInfo(ctx, hc, acct.Brand, tok.Token)
	if err != nil {
		return fmt.Errorf("failed to verify user identity: %w", err)
	}
	acct.UserOpenId = info.OpenID
	acct.UserName = info.Name
	return nil
}

func (p *CredentialProvider) selectedCredentialSource(ctx context.Context) (credentialSource, error) {
	if p.selectedSource != nil {
		return p.selectedSource, nil
	}
	if p.defaultAcct == nil {
		return nil, nil
	}
	if _, err := p.ResolveAccount(ctx); err != nil {
		return nil, err
	}
	if p.selectedSource == nil {
		return nil, fmt.Errorf("credential provider resolved an account without selecting a token source")
	}
	return p.selectedSource, nil
}

func resolveTokenFromSource(ctx context.Context, source credentialSource, req TokenSpec) (*TokenResult, error) {
	result, found, err := source.TryResolveToken(ctx, req)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &TokenUnavailableError{Source: source.Name(), Type: req.Type}
	}
	return result, nil
}

// ResolveIdentityHint resolves default/auto identity guidance from the selected source.
// NOTE: Uses sync.Once — only the context from the first call is used for resolution.
// This matches ResolveAccount and keeps identity decisions stable within one CLI invocation.
func (p *CredentialProvider) ResolveIdentityHint(ctx context.Context) (*IdentityHint, error) {
	p.hintOnce.Do(func() {
		p.hint, p.hintErr = p.doResolveIdentityHint(ctx)
	})
	return p.hint, p.hintErr
}

func (p *CredentialProvider) doResolveIdentityHint(ctx context.Context) (*IdentityHint, error) {
	acct, err := p.ResolveAccount(ctx)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		return &IdentityHint{}, nil
	}
	source, err := p.selectedCredentialSource(ctx)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return &IdentityHint{}, nil
	}
	hint, err := source.ResolveIdentityHint(ctx, acct)
	if err != nil {
		return nil, err
	}
	if hint == nil {
		return &IdentityHint{}, nil
	}
	return hint, nil
}

// ResolveToken resolves an access token.
func (p *CredentialProvider) ResolveToken(ctx context.Context, req TokenSpec) (*TokenResult, error) {
	source, err := p.selectedCredentialSource(ctx)
	if err != nil {
		return nil, err
	}
	if source != nil {
		return resolveTokenFromSource(ctx, source, req)
	}

	for _, prov := range p.providers {
		source := extensionTokenSource{provider: prov}
		result, found, err := source.TryResolveToken(ctx, req)
		if err != nil {
			return nil, err
		}
		if found {
			return result, nil
		}
	}
	source = defaultTokenSource{resolver: p.defaultToken}
	result, found, err := source.TryResolveToken(ctx, req)
	if err != nil {
		return nil, err
	}
	if found {
		return result, nil
	}
	return nil, &TokenUnavailableError{Type: req.Type}
}

// ActiveExtensionProviderName reports whether an extension provider is managing
// credentials. It probes p.providers (extension providers only, not defaultAcct)
// and returns the name of the first engaged provider.
//
// "Engaged" means: ResolveAccount returns a non-nil account, OR returns a
// *extcred.BlockError (provider configured but misconfigured — still counts as
// external). Any other error is propagated to the caller.
//
// Returns ("", nil) when no extension provider is active (built-in keychain path).
// Safe to call multiple times — probes providers directly without the sync.Once cache.
func (p *CredentialProvider) ActiveExtensionProviderName(ctx context.Context) (string, error) {
	for _, prov := range p.providers {
		acct, err := prov.ResolveAccount(ctx)
		if err != nil {
			var blockErr *extcred.BlockError
			if errors.As(err, &blockErr) {
				name := blockErr.Provider
				if name == "" {
					name = prov.Name()
				}
				if name == "" {
					name = "external"
				}
				return name, nil
			}
			return "", err
		}
		if acct != nil {
			if name := prov.Name(); name != "" {
				return name, nil
			}
			return "external", nil
		}
	}
	return "", nil
}

func convertAccount(ext *extcred.Account) *Account {
	return &Account{
		AppID:               ext.AppID,
		AppSecret:           ext.AppSecret,
		Brand:               core.LarkBrand(ext.Brand),
		DefaultAs:           core.Identity(ext.DefaultAs),
		ProfileName:         ext.ProfileName,
		UserOpenId:          ext.OpenID,
		SupportedIdentities: uint8(ext.SupportedIdentities),
	}
}

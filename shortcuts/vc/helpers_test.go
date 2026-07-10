// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package vc

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/credential"
	"github.com/larksuite/cli/shortcuts/common"
)

type meetingQueryTokenResolver struct {
	result *credential.TokenResult
	err    error
}

func (r *meetingQueryTokenResolver) ResolveToken(context.Context, credential.TokenSpec) (*credential.TokenResult, error) {
	return r.result, r.err
}

func bareMeetingQueryRuntime(as core.Identity) *common.RuntimeContext {
	return common.TestNewRuntimeContextWithIdentity(&cobra.Command{Use: "test"}, defaultConfig(), as)
}

func newMeetingQueryRuntime(as core.Identity, resolver *meetingQueryTokenResolver) *common.RuntimeContext {
	runtime := bareMeetingQueryRuntime(as)
	runtime.Factory = &cmdutil.Factory{
		Credential: credential.NewCredentialProvider(nil, nil, resolver, nil),
	}
	return runtime
}

func assertMeetingQueryPermissionError(t *testing.T, err error, identity core.Identity) {
	t.Helper()

	var pe *errs.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *errs.PermissionError, got %T: %v", err, err)
	}
	if pe.Category != errs.CategoryAuthorization {
		t.Fatalf("Category = %q, want %q", pe.Category, errs.CategoryAuthorization)
	}
	if pe.Subtype != errs.SubtypeMissingScope {
		t.Fatalf("Subtype = %q, want %q", pe.Subtype, errs.SubtypeMissingScope)
	}
	if pe.Identity != string(identity) {
		t.Fatalf("Identity = %q, want %q", pe.Identity, identity)
	}
	if !reflect.DeepEqual(pe.MissingScopes, meetingQueryAnyScopes) {
		t.Fatalf("MissingScopes = %v, want %v", pe.MissingScopes, meetingQueryAnyScopes)
	}
	if !strings.Contains(pe.Error(), strings.Join(meetingQueryAnyScopes, ", ")) {
		t.Fatalf("error %q does not mention meeting query scopes %v", pe.Error(), meetingQueryAnyScopes)
	}
	if !strings.Contains(pe.Hint, "auth login --scope") {
		t.Fatalf("Hint = %q, want auth login guidance", pe.Hint)
	}
	if !strings.Contains(pe.Hint, meetingQueryUserScope) || !strings.Contains(pe.Hint, meetingQueryBotScope) {
		t.Fatalf("Hint = %q, want both accepted meeting query scopes", pe.Hint)
	}
}

func TestCheckMeetingQueryAnyScope_AllowsEitherScopeForBothIdentities(t *testing.T) {
	cases := []struct {
		name     string
		identity core.Identity
		scopes   string
	}{
		{name: "user_only_event", identity: core.AsUser, scopes: meetingQueryUserScope},
		{name: "user_only_join", identity: core.AsUser, scopes: meetingQueryBotScope},
		{name: "user_both", identity: core.AsUser, scopes: strings.Join(meetingQueryAnyScopes, " ")},
		{name: "bot_only_event", identity: core.AsBot, scopes: meetingQueryUserScope},
		{name: "bot_only_join", identity: core.AsBot, scopes: meetingQueryBotScope},
		{name: "bot_both", identity: core.AsBot, scopes: strings.Join(meetingQueryAnyScopes, " ")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newMeetingQueryRuntime(tc.identity, &meetingQueryTokenResolver{
				result: &credential.TokenResult{
					Token:  "test-token",
					Scopes: tc.scopes,
				},
			})
			if err := checkMeetingQueryAnyScope(context.Background(), runtime); err != nil {
				t.Fatalf("checkMeetingQueryAnyScope() error = %v, want nil", err)
			}
		})
	}
}

func TestCheckMeetingQueryAnyScope_MissingScopesReturnsPermissionError(t *testing.T) {
	cases := []struct {
		name     string
		identity core.Identity
	}{
		{name: "user", identity: core.AsUser},
		{name: "bot", identity: core.AsBot},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := newMeetingQueryRuntime(tc.identity, &meetingQueryTokenResolver{
				result: &credential.TokenResult{
					Token:  "test-token",
					Scopes: "calendar:calendar:read",
				},
			})
			err := checkMeetingQueryAnyScope(context.Background(), runtime)
			if err == nil {
				t.Fatal("expected permission error, got nil")
			}
			assertMeetingQueryPermissionError(t, err, tc.identity)
		})
	}
}

func TestCheckMeetingQueryAnyScope_IsLenientWhenLocalScopeStateIsUnavailable(t *testing.T) {
	cases := []struct {
		name        string
		makeRuntime func() *common.RuntimeContext
	}{
		{
			name: "nil_runtime",
			makeRuntime: func() *common.RuntimeContext {
				return nil
			},
		},
		{
			name: "nil_factory",
			makeRuntime: func() *common.RuntimeContext {
				return bareMeetingQueryRuntime(core.AsUser)
			},
		},
		{
			name: "nil_credential",
			makeRuntime: func() *common.RuntimeContext {
				runtime := bareMeetingQueryRuntime(core.AsUser)
				runtime.Factory = &cmdutil.Factory{}
				return runtime
			},
		},
		{
			name: "resolver_error",
			makeRuntime: func() *common.RuntimeContext {
				return newMeetingQueryRuntime(core.AsUser, &meetingQueryTokenResolver{
					err: errors.New("boom"),
				})
			},
		},
		{
			name: "empty_scopes",
			makeRuntime: func() *common.RuntimeContext {
				return newMeetingQueryRuntime(core.AsUser, &meetingQueryTokenResolver{
					result: &credential.TokenResult{Token: "test-token"},
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkMeetingQueryAnyScope(context.Background(), tc.makeRuntime()); err != nil {
				t.Fatalf("checkMeetingQueryAnyScope() error = %v, want nil", err)
			}
		})
	}
}

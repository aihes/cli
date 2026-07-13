// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package vc

import (
	"context"
	"strings"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/auth"
	"github.com/larksuite/cli/internal/credential"
	"github.com/larksuite/cli/shortcuts/common"
)

const (
	meetingQueryUserScope = "vc:meeting.meetingevent:read"
	meetingQueryBotScope  = "vc:meeting.bot.join:write"
)

// meetingQueryAnyScopes are the scopes accepted by the VC meeting query
// commands (+meeting-list-active, +meeting-events). They are OR, not AND:
// the upstream APIs authorize the call as long as the caller holds ONE of
// them — a user_access_token granted vc:meeting.meetingevent:read, or the
// bot flow granted vc:meeting.bot.join:write.
//
// The shortcut framework's Scopes/UserScopes/BotScopes preflight is AND, so
// it cannot express "any of these". Those commands therefore leave the
// unconditional scope fields empty and call checkMeetingQueryAnyScope from
// Validate instead.
var meetingQueryAnyScopes = []string{
	meetingQueryUserScope,
	meetingQueryBotScope,
}

// checkMeetingQueryAnyScope succeeds when the resolved identity holds at least
// one scope in meetingQueryAnyScopes. Wire it into a shortcut's Validate (and
// keep Scopes/UserScopes/BotScopes empty) to get OR-style scope preflight.
//
// It is intentionally lenient: when the token or its scope set cannot be
// resolved locally, it returns nil and lets the remote API be the source of
// truth, instead of blocking a call the server might still allow.
func checkMeetingQueryAnyScope(ctx context.Context, runtime *common.RuntimeContext) error {
	if runtime == nil || runtime.Factory == nil || runtime.Factory.Credential == nil || runtime.Config == nil {
		return nil
	}
	// Resolve the identity's granted scopes. If anything about the token cannot
	// be resolved locally, skip the preflight and let the remote API be the
	// source of truth, instead of blocking a call the server might still allow.
	result, err := runtime.Factory.Credential.ResolveToken(ctx, credential.NewTokenSpec(runtime.As(), runtime.Config.AppID))
	if err != nil {
		return nil //nolint:nilerr // intentional: fall back to remote authorization
	}
	if result == nil || result.Scopes == "" {
		return nil
	}
	if hasAnyGrantedScope(result.Scopes, meetingQueryAnyScopes) {
		return nil
	}
	// The APIs accept either scope, but they are granted per identity: a user
	// token carries vc:meeting.meetingevent:read, the bot flow carries
	// vc:meeting.bot.join:write. missing_scopes / Hint feed the AI self-heal
	// path (auth login --scope ...), so only surface the scope the current
	// identity can actually obtain; reporting both would send a user identity
	// after the bot-only scope and dead-end the retry.
	recommended := meetingQueryUserScope
	if runtime.As().IsBot() {
		recommended = meetingQueryBotScope
	}
	return errs.NewPermissionError(
		errs.SubtypeMissingScope,
		"missing one of required scope(s): %s",
		strings.Join(meetingQueryAnyScopes, ", "),
	).
		WithHint("run `lark-cli auth login --scope %q` in the background. It blocks and outputs a verification URL — retrieve the URL and open it in a browser to complete login.", recommended).
		WithMissingScopes(recommended).
		WithIdentity(string(runtime.As()))
}

func hasAnyGrantedScope(granted string, candidates []string) bool {
	for _, scope := range candidates {
		if len(auth.MissingScopes(granted, []string{scope})) == 0 {
			return true
		}
	}
	return false
}

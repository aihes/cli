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

var meetingQueryAnyScopes = []string{
	"vc:meeting.meetingevent:read",
	"vc:meeting.bot.join:write",
}

func checkMeetingQueryAnyScope(ctx context.Context, runtime *common.RuntimeContext) error {
	if runtime == nil || runtime.Factory == nil || runtime.Factory.Credential == nil || runtime.Config == nil {
		return nil
	}
	result, err := runtime.Factory.Credential.ResolveToken(ctx, credential.NewTokenSpec(runtime.As(), runtime.Config.AppID))
	if err != nil || result == nil || result.Scopes == "" {
		return nil
	}
	if hasAnyGrantedScope(result.Scopes, meetingQueryAnyScopes) {
		return nil
	}
	return errs.NewPermissionError(
		errs.SubtypeMissingScope,
		"missing one of required scope(s): %s",
		strings.Join(meetingQueryAnyScopes, ", "),
	).
		WithHint("run `lark-cli auth login --scope %q` in the background. It blocks and outputs a verification URL — retrieve the URL and open it in a browser to complete login.", strings.Join(meetingQueryAnyScopes, " ")).
		WithMissingScopes(meetingQueryAnyScopes...).
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

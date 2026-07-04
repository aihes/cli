// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package errclass

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/policystate"
)

// Permission recovery hints point at `auth login`; with the auth domain
// absent from the build those pointers must not render. The tenant-admin and
// console-side hints are not auth-login recoveries and stay.
func TestPermissionHint_followsAuthDomain(t *testing.T) {
	policystate.ResetForTesting()
	t.Cleanup(policystate.ResetForTesting)

	for _, st := range []errs.Subtype{errs.SubtypeMissingScope, errs.SubtypeTokenScopeInsufficient, errs.SubtypeUserUnauthorized} {
		if h := PermissionHint([]string{"im:message"}, "user", st, ""); !strings.Contains(h, "auth login") {
			t.Errorf("%s: default build hint should mention auth login, got %q", st, h)
		}
	}

	policystate.SetPluginDeniedDomains(map[string]bool{"auth": true})
	for _, st := range []errs.Subtype{errs.SubtypeMissingScope, errs.SubtypeTokenScopeInsufficient, errs.SubtypeUserUnauthorized} {
		if h := PermissionHint([]string{"im:message"}, "user", st, ""); strings.Contains(h, "auth login") {
			t.Errorf("%s: auth-denied build must not point at auth login, got %q", st, h)
		}
	}
	// Non-auth-login recoveries are untouched.
	if h := PermissionHint(nil, "bot", errs.SubtypeAppScopeNotApplied, "https://example.com"); !strings.Contains(h, "developer console") {
		t.Errorf("console hint must survive the auth gate, got %q", h)
	}
}

// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"strings"
	"testing"

	"github.com/larksuite/cli/internal/policystate"
)

// The auth-login recovery hint points into the auth domain; when an
// integrator plugin denied that whole domain the hint would be a dead
// end, so it stays off the error.
func TestNeedUserAuthorization_hintFollowsAuthDomain(t *testing.T) {
	t.Cleanup(policystate.ResetForTesting)

	policystate.SetPluginDeniedDomains(nil)
	if e := NewNeedUserAuthorizationError("ou_x"); !strings.Contains(e.Hint, "auth login") {
		t.Errorf("default build must keep the auth login hint, got %q", e.Hint)
	}

	policystate.SetPluginDeniedDomains(map[string]bool{"auth": true})
	if e := NewNeedUserAuthorizationError("ou_x"); e.Hint != "" {
		t.Errorf("auth-denied build must not steer to auth login, got %q", e.Hint)
	}
}

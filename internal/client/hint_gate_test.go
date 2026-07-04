// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package client

import (
	"errors"
	"strings"
	"testing"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/core"
	"github.com/larksuite/cli/internal/policystate"
)

// Same gate as internal/auth: the token-missing recovery hint points into
// the auth domain and stays off the error when a plugin denied it.
func TestTokenMissing_hintFollowsAuthDomain(t *testing.T) {
	t.Cleanup(policystate.ResetForTesting)

	policystate.SetPluginDeniedDomains(nil)
	var ae *errs.AuthenticationError
	if err := newTokenMissingError(core.AsUser, nil); !errors.As(err, &ae) || !strings.Contains(ae.Hint, "auth login") {
		t.Errorf("default build must keep the auth login hint, got %v", err)
	}

	policystate.SetPluginDeniedDomains(map[string]bool{"auth": true})
	if err := newTokenMissingError(core.AsUser, nil); !errors.As(err, &ae) || ae.Hint != "" {
		t.Errorf("auth-denied build must not steer to auth login, got %v", err)
	}
}

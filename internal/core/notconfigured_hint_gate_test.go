// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/policystate"
)

// The not-configured recovery hint points at `config init`/`config bind`;
// with the config domain absent from the build it must not render. The error
// itself (subtype, message) is unchanged.
func TestNotConfiguredError_hintFollowsConfigDomain(t *testing.T) {
	policystate.ResetForTesting()
	t.Cleanup(policystate.ResetForTesting)

	var ce *errs.ConfigError
	if err := NotConfiguredError(); !errors.As(err, &ce) || ce.Hint == "" || !strings.Contains(ce.Hint, "config") {
		t.Fatalf("default build must hint at a config command, got %+v", err)
	}

	policystate.SetPluginDeniedDomains(map[string]bool{"config": true})
	if err := NotConfiguredError(); !errors.As(err, &ce) {
		t.Fatalf("expected *errs.ConfigError, got %T", err)
	} else {
		if ce.Subtype != errs.SubtypeNotConfigured {
			t.Errorf("subtype = %q, want not_configured (gate must not change the error)", ce.Subtype)
		}
		if ce.Hint != "" {
			t.Errorf("config-denied build must omit the recovery hint, got %q", ce.Hint)
		}
	}
}

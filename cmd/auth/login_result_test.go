// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"errors"
	"reflect"
	"testing"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/cmdutil"
)

// TestHandleLoginScopeIssue_FailedJSON_PreservesScopeTriple asserts that the
// failed-login JSON branch (loginSucceeded == false, opts.JSON == true) wires
// requested + granted + missing scopes into the typed *PermissionError
// envelope. Consumers need the full triple to render actionable diagnostics,
// not just the missing set.
func TestHandleLoginScopeIssue_FailedJSON_PreservesScopeTriple(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	f, _, _, _ := cmdutil.TestFactory(t, nil)

	requested := []string{"docx:document", "im:message:send"}
	granted := []string{"docx:document"}
	missing := []string{"im:message:send"}

	err := handleLoginScopeIssue(
		&LoginOptions{JSON: true},
		getLoginMsg("en"),
		f,
		&loginScopeIssue{
			Message: "scope insufficient",
			Hint:    "re-login with --scope im:message:send",
			Summary: &loginScopeSummary{
				Requested: requested,
				Granted:   granted,
				Missing:   missing,
			},
		},
		"", // openId empty -> loginSucceeded = false
		"tester",
		"", // statusMessage unused on the failed-login path
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var permErr *errs.PermissionError
	if !errors.As(err, &permErr) {
		t.Fatalf("expected *errs.PermissionError, got %T: %v", err, err)
	}
	if !reflect.DeepEqual(permErr.RequestedScopes, requested) {
		t.Errorf("RequestedScopes = %v, want %v", permErr.RequestedScopes, requested)
	}
	if !reflect.DeepEqual(permErr.GrantedScopes, granted) {
		t.Errorf("GrantedScopes = %v, want %v", permErr.GrantedScopes, granted)
	}
	if !reflect.DeepEqual(permErr.MissingScopes, missing) {
		t.Errorf("MissingScopes = %v, want %v", permErr.MissingScopes, missing)
	}
}

// TestAuthorizationCompletePayload_StatusMessage asserts that the
// authorization response's status_message text is passed through into the
// JSON payload verbatim (CLI does not parse/classify/truncate it), and
// that the field is always present, using an empty string when there is no
// message, consistent with the stable-output-shape convention already used
// by "scope" and the other summary fields.
func TestAuthorizationCompletePayload_StatusMessage(t *testing.T) {
	summary := &loginScopeSummary{Granted: []string{"a:b:c"}}

	p := authorizationCompletePayload("ou_x", "u", summary, nil, "审批中，请等待管理员处理")
	if p["status_message"] != "审批中，请等待管理员处理" {
		t.Fatalf("status_message = %v", p["status_message"])
	}

	// No message from upstream -> stable empty string, not an absent key.
	p2 := authorizationCompletePayload("ou_x", "u", summary, nil, "")
	if p2["status_message"] != "" {
		t.Fatalf("empty status_message should be \"\", got %v", p2["status_message"])
	}
}

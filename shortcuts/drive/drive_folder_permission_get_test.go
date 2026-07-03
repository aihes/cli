// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package drive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/larksuite/cli/internal/httpmock"
	"github.com/larksuite/cli/shortcuts/common"
)

func newDriveFolderPermissionGetRuntime(t *testing.T, rawURL, folderToken string) *common.RuntimeContext {
	t.Helper()

	cmd := &cobra.Command{Use: "drive +folder-permission-get"}
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("folder-token", "", "")
	if rawURL != "" {
		if err := cmd.Flags().Set("url", rawURL); err != nil {
			t.Fatalf("set --url: %v", err)
		}
	}
	if folderToken != "" {
		if err := cmd.Flags().Set("folder-token", folderToken); err != nil {
			t.Fatalf("set --folder-token: %v", err)
		}
	}
	return common.TestNewRuntimeContext(cmd, driveTestConfig())
}

func TestDriveFolderPermissionGetSpecResolvesFolderURL(t *testing.T) {
	t.Parallel()

	runtime := newDriveFolderPermissionGetRuntime(t, "https://example.feishu.cn/drive/folder/fldTok?from=share", "")
	spec, err := readDriveFolderPermissionGetSpec(runtime)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.FolderToken != "fldTok" {
		t.Fatalf("FolderToken = %q, want fldTok", spec.FolderToken)
	}
}

func TestDriveFolderPermissionGetSpecResolvesBareFolderToken(t *testing.T) {
	t.Parallel()

	runtime := newDriveFolderPermissionGetRuntime(t, "", " fldTok ")
	spec, err := readDriveFolderPermissionGetSpec(runtime)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if spec.FolderToken != "fldTok" {
		t.Fatalf("FolderToken = %q, want fldTok", spec.FolderToken)
	}
}

func TestDriveFolderPermissionGetSpecValidationErrorsAreTyped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rawURL      string
		folderToken string
		wantParam   string
		wantMessage string
	}{
		{
			name:        "missing locator",
			wantParam:   "--url",
			wantMessage: "pass exactly one",
		},
		{
			name:        "mutually exclusive locators",
			rawURL:      "https://example.feishu.cn/drive/folder/fldTok",
			folderToken: "fldTok",
			wantParam:   "--url",
			wantMessage: "mutually exclusive",
		},
		{
			name:        "non-folder URL",
			rawURL:      "https://example.feishu.cn/docx/doxTok",
			wantParam:   "--url",
			wantMessage: "must point to a Drive folder",
		},
		{
			name:        "unsupported URL",
			rawURL:      "https://example.feishu.cn/calendar/calTok",
			wantParam:   "--url",
			wantMessage: "unsupported --url",
		},
		{
			name:        "invalid bare folder token",
			folderToken: "../bad",
			wantParam:   "--folder-token",
			wantMessage: "--folder-token",
		},
	}

	for _, temp := range tests {
		tt := temp
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runtime := newDriveFolderPermissionGetRuntime(t, tt.rawURL, tt.folderToken)
			_, err := readDriveFolderPermissionGetSpec(runtime)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			problem, ok := errs.ProblemOf(err)
			if !ok {
				t.Fatalf("error is not typed: %T %v", err, err)
			}
			if problem.Category != errs.CategoryValidation || problem.Subtype != errs.SubtypeInvalidArgument {
				t.Fatalf("problem = %s/%s, want validation/invalid_argument", problem.Category, problem.Subtype)
			}
			if validationErr, ok := err.(*errs.ValidationError); ok {
				if validationErr.Param != tt.wantParam {
					t.Fatalf("param = %q, want %q", validationErr.Param, tt.wantParam)
				}
			} else {
				t.Fatalf("error type = %T, want *errs.ValidationError", err)
			}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantMessage)
			}
		})
	}
}

func TestDriveFolderPermissionGetDryRunIncludesGETRequest(t *testing.T) {
	t.Parallel()

	runtime := newDriveFolderPermissionGetRuntime(t, "https://example.feishu.cn/drive/folder/fldTok", "")
	dry := DriveFolderPermissionGet.DryRun(context.Background(), runtime)
	if dry == nil {
		t.Fatal("DryRun returned nil")
	}
	data, err := json.Marshal(dry)
	if err != nil {
		t.Fatalf("marshal dry-run: %v", err)
	}
	out := string(data)
	for _, want := range []string{
		`"/open-apis/drive/v2/permissions/fldTok/public"`,
		`"GET"`,
		`"type":"folder"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `"folder_token"`) {
		t.Fatalf("dry-run output contains folder_token, want omitted:\n%s", out)
	}
}

func TestDriveFolderPermissionGetExecutePreservesPermissionPublic(t *testing.T) {
	f, stdout, _, reg := cmdutil.TestFactory(t, driveTestConfig())
	reg.Register(&httpmock.Stub{
		Method: "GET",
		URL:    "/open-apis/drive/v2/permissions/fldTok/public?type=folder",
		Body: map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"permission_public": map[string]interface{}{
					"link_share_entity":          "closed",
					"external_access_entity":     "closed",
					"security_entity":            "anyone_can_view",
					"comment_entity":             "anyone_can_view",
					"share_entity":               "anyone",
					"manage_collaborator_entity": "collaborator_can_view",
					"lock_switch":                false,
					"server_future_folder_field": "preserved",
				},
			},
		},
	})

	err := mountAndRunDrive(t, DriveFolderPermissionGet, []string{
		"+folder-permission-get",
		"--folder-token", "fldTok",
		"--as", "bot",
	}, f, stdout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data := decodeDriveEnvelope(t, stdout)
	for _, key := range []string{"type", "folder_token", "url"} {
		if _, ok := data[key]; ok {
			t.Fatalf("data[%s] = %#v, want field omitted", key, data[key])
		}
	}
	permissionPublic, _ := data["permission_public"].(map[string]interface{})
	if permissionPublic == nil {
		t.Fatalf("permission_public missing in output: %#v", data)
	}
	for key, want := range map[string]interface{}{
		"link_share_entity":          "closed",
		"external_access_entity":     "closed",
		"security_entity":            "anyone_can_view",
		"comment_entity":             "anyone_can_view",
		"share_entity":               "anyone",
		"manage_collaborator_entity": "collaborator_can_view",
		"lock_switch":                false,
		"server_future_folder_field": "preserved",
	} {
		if permissionPublic[key] != want {
			t.Fatalf("permission_public[%s] = %#v, want %#v", key, permissionPublic[key], want)
		}
	}
}

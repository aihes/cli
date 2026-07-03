// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package drive

import (
	"context"
	"testing"
	"time"

	clie2e "github.com/larksuite/cli/tests/cli_e2e"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDrive_FolderPermissionGetDryRun(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("LARKSUITE_CLI_APP_ID", "app")
	t.Setenv("LARKSUITE_CLI_APP_SECRET", "secret")
	t.Setenv("LARKSUITE_CLI_BRAND", "feishu")

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "folder token",
			args: []string{
				"drive", "+folder-permission-get",
				"--folder-token", "fldE2E001",
				"--dry-run",
			},
		},
		{
			name: "folder URL",
			args: []string{
				"drive", "+folder-permission-get",
				"--url", "https://example.feishu.cn/drive/folder/fldE2E001?from=share",
				"--dry-run",
			},
		},
	}

	for _, temp := range tests {
		tt := temp
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)

			result, err := clie2e.RunCmd(ctx, clie2e.Request{
				Args:      tt.args,
				DefaultAs: "bot",
			})
			require.NoError(t, err)
			result.AssertExitCode(t, 0)

			out := result.Stdout
			if got := gjson.Get(out, "api.0.method").String(); got != "GET" {
				t.Fatalf("method = %q, want GET\nstdout:\n%s", got, out)
			}
			if got := gjson.Get(out, "api.0.url").String(); got != "/open-apis/drive/v2/permissions/fldE2E001/public" {
				t.Fatalf("url = %q, want v2 folder permission public endpoint\nstdout:\n%s", got, out)
			}
			if got := gjson.Get(out, "api.0.params.type").String(); got != "folder" {
				t.Fatalf("params.type = %q, want folder\nstdout:\n%s", got, out)
			}
			if gjson.Get(out, "folder_token").Exists() {
				t.Fatalf("folder_token exists in dry-run output, want omitted\nstdout:\n%s", out)
			}
		})
	}
}

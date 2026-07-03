// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package drive

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/internal/validate"
	"github.com/larksuite/cli/shortcuts/common"
)

type driveFolderPermissionGetSpec struct {
	FolderToken string
}

func readDriveFolderPermissionGetSpec(runtime *common.RuntimeContext) (driveFolderPermissionGetSpec, error) {
	rawURL := strings.TrimSpace(runtime.Str("url"))
	rawToken := strings.TrimSpace(runtime.Str("folder-token"))

	if rawURL == "" && rawToken == "" {
		return driveFolderPermissionGetSpec{}, errs.NewValidationError(
			errs.SubtypeInvalidArgument,
			"pass exactly one of --url or --folder-token",
		).WithParam("--url")
	}
	if rawURL != "" && rawToken != "" {
		return driveFolderPermissionGetSpec{}, errs.NewValidationError(
			errs.SubtypeInvalidArgument,
			"--url and --folder-token are mutually exclusive; pass only one folder locator",
		).WithParam("--url")
	}

	if rawToken != "" {
		if err := validate.ResourceName(rawToken, "--folder-token"); err != nil {
			return driveFolderPermissionGetSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument, "%s", err).WithParam("--folder-token")
		}
		return driveFolderPermissionGetSpec{FolderToken: rawToken}, nil
	}

	ref, ok := common.ParseResourceURL(rawURL)
	if !ok {
		return driveFolderPermissionGetSpec{}, errs.NewValidationError(
			errs.SubtypeInvalidArgument,
			"unsupported --url %q: pass a recognized Lark Drive folder URL such as https://example.feishu.cn/drive/folder/<folder_token>",
			rawURL,
		).WithParam("--url")
	}
	if ref.Type != "folder" {
		return driveFolderPermissionGetSpec{}, errs.NewValidationError(
			errs.SubtypeInvalidArgument,
			"--url must point to a Drive folder; got resource type %q",
			ref.Type,
		).WithParam("--url")
	}
	if err := validate.ResourceName(ref.Token, "--url"); err != nil {
		return driveFolderPermissionGetSpec{}, errs.NewValidationError(errs.SubtypeInvalidArgument, "%s", err).WithParam("--url")
	}
	return driveFolderPermissionGetSpec{FolderToken: ref.Token}, nil
}

func (s driveFolderPermissionGetSpec) url(runtime *common.RuntimeContext) string {
	if runtime != nil && runtime.Config != nil {
		if u := common.BuildResourceURL(runtime.Config.Brand, "folder", s.FolderToken); u != "" {
			return u
		}
	}
	return common.BuildResourceURL("", "folder", s.FolderToken)
}

func (s driveFolderPermissionGetSpec) params() map[string]interface{} {
	return map[string]interface{}{"type": "folder"}
}

func (s driveFolderPermissionGetSpec) apiPath() string {
	return drivePermissionPublicV2Path(s.FolderToken)
}

func drivePermissionPublicV2Path(token string) string {
	return fmt.Sprintf("/open-apis/drive/v2/permissions/%s/public", validate.EncodePathSegment(token))
}

func (s driveFolderPermissionGetSpec) output(runtime *common.RuntimeContext, data map[string]interface{}) map[string]interface{} {
	permissionPublic := interface{}(data)
	if nestedPermissionPublic := common.GetMap(data, "permission_public"); nestedPermissionPublic != nil {
		permissionPublic = nestedPermissionPublic
	}
	return map[string]interface{}{
		"permission_public": permissionPublic,
	}
}

// DriveFolderPermissionGet queries the permission_public settings for a Drive
// folder itself.
var DriveFolderPermissionGet = common.Shortcut{
	Service:     "drive",
	Command:     "+folder-permission-get",
	Description: "Get a Drive folder's sharing, copy, download, and comment permission settings",
	Risk:        "read",
	AuthTypes:   []string{"user", "bot"},
	HasFormat:   true,
	Flags: []common.Flag{
		{Name: "url", Desc: "Drive folder URL, for example https://example.feishu.cn/drive/folder/<folder_token>"},
		{Name: "folder-token", Desc: "Drive folder token; mutually exclusive with --url"},
	},
	Tips: []string{
		"Pass exactly one of --url or --folder-token.",
		"This shortcut reads the folder's own permission settings; it does not list child document permissions.",
	},
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := readDriveFolderPermissionGetSpec(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		spec, err := readDriveFolderPermissionGetSpec(runtime)
		if err != nil {
			return common.NewDryRunAPI().Set("error", err.Error())
		}
		return common.NewDryRunAPI().
			Desc("Get Drive folder permission settings").
			GET(spec.apiPath()).
			Params(spec.params())
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		spec, err := readDriveFolderPermissionGetSpec(runtime)
		if err != nil {
			return err
		}

		fmt.Fprintf(runtime.IO().ErrOut, "Getting permission settings for folder %s...\n", common.MaskToken(spec.FolderToken))
		data, err := runtime.CallAPITyped(
			"GET",
			spec.apiPath(),
			spec.params(),
			nil,
		)
		if err != nil {
			return err
		}

		out := spec.output(runtime, data)
		runtime.OutFormat(out, nil, func(w io.Writer) {
			fmt.Fprintf(w, "Type:         folder\n")
			fmt.Fprintf(w, "FolderToken:  %s\n", spec.FolderToken)
			fmt.Fprintf(w, "URL:          %s\n", spec.url(runtime))
		})
		return nil
	},
}

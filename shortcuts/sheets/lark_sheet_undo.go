// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"context"

	"github.com/larksuite/cli/shortcuts/common"
)

var Undo = common.Shortcut{
	Service:     "sheets",
	Command:     "+undo",
	Description: "Undo the current user's latest spreadsheet write.",
	Risk:        "write",
	Scopes:      []string{"sheets:spreadsheet:write_only"},
	AuthTypes:   []string{"user"},
	HasFormat:   true,
	Flags:       historyLocatorFlags(),
	Validate: func(ctx context.Context, runtime *common.RuntimeContext) error {
		_, err := resolveSpreadsheetToken(runtime)
		return err
	},
	DryRun: func(ctx context.Context, runtime *common.RuntimeContext) *common.DryRunAPI {
		token, _ := resolveSpreadsheetToken(runtime)
		return invokeToolDryRun(token, ToolKindWrite, "undo_last", undoInput(token))
	},
	Execute: func(ctx context.Context, runtime *common.RuntimeContext) error {
		token, err := resolveSpreadsheetTokenExec(runtime)
		if err != nil {
			return err
		}
		out, err := callTool(ctx, runtime, token, ToolKindWrite, "undo_last", undoInput(token))
		if err != nil {
			return err
		}
		runtime.Out(out, nil)
		return nil
	},
}

func undoInput(token string) map[string]interface{} {
	return map[string]interface{}{
		"excel_id": token,
	}
}

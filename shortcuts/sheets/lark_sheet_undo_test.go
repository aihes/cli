// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"strings"
	"testing"
)

func TestUndo_DryRun(t *testing.T) {
	t.Parallel()

	args := []string{"--url", testURL, "--as", "user"}
	callURL := dryRunFirstCallURL(t, Undo, args)
	if !containsSuffix(callURL, "invoke_write") {
		t.Errorf("invoke url = %q, want invoke_write", callURL)
	}

	body := parseDryRunBody(t, Undo, args)
	got := decodeToolInput(t, body, "undo_last")
	assertInputEquals(t, got, map[string]interface{}{
		"excel_id": testToken,
		"count":    float64(1),
	})
}

func TestExecute_Undo(t *testing.T) {
	t.Parallel()

	stub := toolOutputStub(testToken, "write", `{"undone":3,"op_ids":["op-3","op-2","op-1"],"results":[{"op_id":"op-3"},{"op_id":"op-2"},{"op_id":"op-1"}]}`)
	out, err := runShortcutWithStubs(t, Undo, []string{"--url", testURL, "--count", "3", "--as", "user"}, stub)
	if err != nil {
		t.Fatalf("execute failed: %v\nout=%s", err, out)
	}

	body := decodeRawEnvelopeBody(t, stub.CapturedBody)
	input := decodeToolInput(t, body, "undo_last")
	assertInputEquals(t, input, map[string]interface{}{
		"excel_id": testToken,
		"count":    float64(3),
	})

	data := decodeEnvelopeData(t, out)
	if data["undone"].(float64) != 3 {
		t.Fatalf("unexpected output data: %#v", data)
	}
}

func TestUndo_ValidateCount(t *testing.T) {
	t.Parallel()

	_, err := runShortcutWithStubs(t, Undo, []string{"--url", testURL, "--count", "21", "--as", "user"})
	if err == nil {
		t.Fatal("expected count validation error")
	}
	if !strings.Contains(err.Error(), "--count must be between 1 and 20") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import "testing"

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
	})
}

func TestExecute_Undo(t *testing.T) {
	t.Parallel()

	stub := toolOutputStub(testToken, "write", `{"undone":1,"op_id":"op-1","top_doc_revision":2,"new_revision":3}`)
	out, err := runShortcutWithStubs(t, Undo, []string{"--url", testURL, "--as", "user"}, stub)
	if err != nil {
		t.Fatalf("execute failed: %v\nout=%s", err, out)
	}

	body := decodeRawEnvelopeBody(t, stub.CapturedBody)
	input := decodeToolInput(t, body, "undo_last")
	assertInputEquals(t, input, map[string]interface{}{
		"excel_id": testToken,
	})

	data := decodeEnvelopeData(t, out)
	if data["undone"].(float64) != 1 || data["op_id"] != "op-1" {
		t.Fatalf("unexpected output data: %#v", data)
	}
}

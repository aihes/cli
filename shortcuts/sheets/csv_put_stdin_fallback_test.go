// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package sheets

import (
	"testing"

	"github.com/larksuite/cli/internal/cmdutil"
	"github.com/spf13/cobra"
)

// +csv-put lets a piped CSV satisfy an omitted --csv: agents routinely redirect
// a file into stdin but forget the `--csv -`. PostMount relaxes the required
// gate and installs a PreRunE that, when stdin is a non-interactive pipe,
// defaults an absent --csv to "-" so the standard stdin path reads it. On an
// interactive terminal the fallback stays off so the command never blocks.
func mountCsvPut(t *testing.T) *cobra.Command {
	t.Helper()
	f, _, _, _ := cmdutil.TestFactory(t, nil)
	parent := &cobra.Command{Use: "sheets"}
	CsvPut.Mount(parent, f)
	cmd, _, err := parent.Find([]string{"+csv-put"})
	if err != nil {
		t.Fatalf("Find(+csv-put) error = %v", err)
	}
	return cmd
}

func csvRequiredAnnotationPresent(cmd *cobra.Command) bool {
	fl := cmd.Flags().Lookup("csv")
	if fl == nil {
		return false
	}
	_, ok := fl.Annotations[cobra.BashCompOneRequiredFlag]
	return ok
}

// withStdinIsPipe swaps the package-level pipe detector for the duration of a
// test so behavior does not depend on the real process stdin.
func withStdinIsPipe(t *testing.T, piped bool) {
	t.Helper()
	prev := csvPutStdinIsPipe
	csvPutStdinIsPipe = func() bool { return piped }
	t.Cleanup(func() { csvPutStdinIsPipe = prev })
}

func TestCsvPutPostMount_RelaxesCsvRequired(t *testing.T) {
	cmd := mountCsvPut(t)
	if csvRequiredAnnotationPresent(cmd) {
		t.Error("--csv required annotation should be relaxed so csvPutInput reports the typed error")
	}
	if cmd.PreRunE == nil {
		t.Fatal("PostMount should install a PreRunE for the stdin fallback")
	}
}

func TestCsvPutPreRunE_PipedAbsentDefaultsToDash(t *testing.T) {
	withStdinIsPipe(t, true)
	cmd := mountCsvPut(t)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE error = %v", err)
	}
	if got, _ := cmd.Flags().GetString("csv"); got != "-" {
		t.Errorf("csv = %q, want %q (piped + absent should default to '-')", got, "-")
	}
}

func TestCsvPutPreRunE_InteractiveAbsentStaysEmpty(t *testing.T) {
	withStdinIsPipe(t, false)
	cmd := mountCsvPut(t)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE error = %v", err)
	}
	if got, _ := cmd.Flags().GetString("csv"); got != "" {
		t.Errorf("csv = %q, want empty (interactive stdin must not be consumed / must not hang)", got)
	}
}

func TestCsvPutPreRunE_ExplicitValueUnchanged(t *testing.T) {
	withStdinIsPipe(t, true)
	cmd := mountCsvPut(t)
	if err := cmd.Flags().Set("csv", "x,y"); err != nil {
		t.Fatalf("Set(csv) error = %v", err)
	}
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE error = %v", err)
	}
	if got, _ := cmd.Flags().GetString("csv"); got != "x,y" {
		t.Errorf("csv = %q, want %q (explicit value must not be overridden)", got, "x,y")
	}
}

func TestCsvPutPostMount_ComposesExistingPreRunE(t *testing.T) {
	withStdinIsPipe(t, true)
	cmd := &cobra.Command{Use: "+csv-put"}
	cmd.Flags().String("start-cell", "", "")
	cmd.Flags().String("range", "", "")
	cmd.Flags().String("csv", "", "")
	called := false
	cmd.PreRunE = func(*cobra.Command, []string) error {
		called = true
		return nil
	}
	CsvPut.PostMount(cmd)
	if err := cmd.PreRunE(cmd, nil); err != nil {
		t.Fatalf("PreRunE error = %v", err)
	}
	if !called {
		t.Fatal("existing PreRunE was not called")
	}
	if got, _ := cmd.Flags().GetString("csv"); got != "-" {
		t.Fatalf("csv = %q, want fallback after existing PreRunE", got)
	}
}

// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/errs"
	"github.com/larksuite/cli/extension/platform"
	"github.com/larksuite/cli/internal/cmdpolicy"
	"github.com/larksuite/cli/internal/core"
	internalplatform "github.com/larksuite/cli/internal/platform"
	"github.com/larksuite/cli/internal/policystate"
	"github.com/larksuite/cli/internal/update"
)

// restrictingPlugin registers a plugin that denies the given globs; extra
// customizes the builder further (nil for none).
func restrictingPlugin(t *testing.T, deny []string, extra func(*platform.Builder) *platform.Builder) {
	t.Helper()
	platform.ResetForTesting()
	t.Cleanup(platform.ResetForTesting)
	t.Cleanup(func() { policystate.ResetForTesting() })
	b := platform.NewPlugin("acme", "1.0").Restrict(&platform.Rule{Deny: deny})
	if extra != nil {
		b = extra(b)
	}
	platform.Register(b.MustBuild())
}

// runnableUnder returns the first runnable non-exempt descendant under
// the named top-level group of the real command tree (the diagnostic
// exemptions keep their original RunE and would not exercise the stub).
func runnableUnder(t *testing.T, root *cobra.Command, group string) *cobra.Command {
	t.Helper()
	g := findByPath(root, group)
	if g == nil {
		t.Fatalf("group %q not found in command tree", group)
	}
	var find func(c *cobra.Command) *cobra.Command
	find = func(c *cobra.Command) *cobra.Command {
		if c.RunE != nil && len(c.Commands()) == 0 && !cmdpolicy.IsDiagnosticPath(cmdpolicy.CanonicalPath(c)) {
			return c
		}
		for _, child := range c.Commands() {
			if leaf := find(child); leaf != nil {
				return leaf
			}
		}
		return nil
	}
	leaf := find(g)
	if leaf == nil {
		t.Fatalf("no runnable non-exempt leaf under %q", group)
	}
	return leaf
}

// A plugin-denied command answers with command_unavailable: default
// message, no hint, no policy vocabulary.
func TestBuildInternal_pluginDenyPresentsUnavailable(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	leaf := runnableUnder(t, root, "config")

	err := leaf.RunE(leaf, nil)
	var ve *errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *errs.ValidationError, got %T %v", err, err)
	}
	if ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("subtype = %q, want command_unavailable", ve.Subtype)
	}
	if ve.Message != cmdpolicy.DefaultUnavailableMessage || ve.Hint != "" {
		t.Errorf("message=%q hint=%q, want default message and empty hint", ve.Message, ve.Hint)
	}
}

// Rule.DeniedMessage speaks in the integrator's product voice.
func TestBuildInternal_deniedMessageOverride(t *testing.T) {
	tmpHome(t)
	platform.ResetForTesting()
	t.Cleanup(platform.ResetForTesting)
	t.Cleanup(func() { policystate.ResetForTesting() })
	platform.Register(platform.NewPlugin("acme", "1.0").
		Restrict(&platform.Rule{Deny: []string{"config/**"}, DeniedMessage: "not part of acme cli"}).
		MustBuild())

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	leaf := runnableUnder(t, root, "config")

	err := leaf.RunE(leaf, nil)
	var ve *errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *errs.ValidationError, got %T", err)
	}
	if ve.Message != "not part of acme cli" {
		t.Errorf("message = %q, want the integrator's DeniedMessage", ve.Message)
	}
}

// Explicit help on a plugin-denied command must not render the original
// usage; it answers with the same unavailable envelope.
func TestBuildInternal_pluginDenyHelpIntercepted(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	leaf := runnableUnder(t, root, "config")

	var buf bytes.Buffer
	leaf.SetOut(&buf)
	leaf.SetErr(&buf)
	root.HelpFunc()(leaf, nil)
	out := buf.String()
	if !strings.Contains(out, "command_unavailable") {
		t.Errorf("explicit help must answer command_unavailable, got:\n%s", out)
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("explicit help must not render the original usage, got:\n%s", out)
	}

	// yaml-source presentation is untouched: a command outside the denied
	// domain still renders normal help.
	var normal bytes.Buffer
	alive := findByPath(root, "skills")
	alive.SetOut(&normal)
	alive.SetErr(&normal)
	root.HelpFunc()(alive, nil)
	if strings.Contains(normal.String(), "command_unavailable") {
		t.Errorf("non-denied command help must render normally, got:\n%s", normal.String())
	}
}

// `lark-cli help <plugin-restricted-cmd>` must fail with the typed
// unavailable error (exit non-zero), not print an envelope and exit 0.
func TestBuildInternal_helpCommandReturnsUnavailable(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	helpCmd := findByPath(root, "help")
	if helpCmd == nil || helpCmd.RunE == nil {
		t.Fatal("custom help command not installed")
	}

	err := helpCmd.RunE(helpCmd, []string{"config"})
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("help on a restricted command must return command_unavailable, got %v", err)
	}

	// A live target renders normally and returns nil.
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := helpCmd.RunE(helpCmd, []string{"skills"}); err != nil {
		t.Errorf("help on a live command must succeed, got %v", err)
	}
}

// help is a framework meta command: an allow-list rule must not deny it.
// `help <live-cmd>` renders; `help <denied-cmd>` returns unavailable.
func TestBuildInternal_helpSurvivesAllowList(t *testing.T) {
	tmpHome(t)
	platform.ResetForTesting()
	t.Cleanup(platform.ResetForTesting)
	t.Cleanup(func() { policystate.ResetForTesting() })
	platform.Register(platform.NewPlugin("acme", "1.0").
		Restrict(&platform.Rule{Allow: []string{"im/**"}, AllowUnannotated: true}).
		MustBuild())

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	helpCmd := findByPath(root, "help")
	if helpCmd == nil || helpCmd.Annotations[cmdpolicy.AnnotationDenialLayer] != "" {
		t.Fatalf("help must not be policy-denied under an allow-list; annotations=%v", helpCmd.Annotations)
	}

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	if err := helpCmd.RunE(helpCmd, []string{"im"}); err != nil {
		t.Errorf("help on an allowed domain must render, got %v", err)
	}

	err := helpCmd.RunE(helpCmd, []string{"config"})
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("help on a denied domain must return command_unavailable, got %v", err)
	}
}

// The plugin inventory surfaces the new contributions so `config plugins
// show` can answer "what did this build customize".
func TestBuildInternal_inventoryCoversNewCapabilities(t *testing.T) {
	tmpHome(t)
	withBaseSkills(t, map[string]string{"lark-a/SKILL.md": "---\ndescription: a\n---\n"})
	restrictingPlugin(t, []string{"profile/**"}, func(b *platform.Builder) *platform.Builder {
		return b.HideDiagnostics().
			EmbeddedSkills(&platform.SkillsOverlay{Remove: []string{"lark-a"}})
	})

	buildInternal(context.Background(), buildInvocationForTest(t))
	inv := internalplatform.GetActiveInventory()
	if inv == nil || len(inv.Plugins) != 1 {
		t.Fatalf("inventory = %+v, want 1 plugin", inv)
	}
	p := inv.Plugins[0]
	if !p.Capabilities.HideDiagnostics {
		t.Error("inventory must surface HideDiagnostics")
	}
	if p.EmbeddedSkills == nil || len(p.EmbeddedSkills.Remove) != 1 || p.EmbeddedSkills.Remove[0] != "lark-a" {
		t.Errorf("inventory must summarise EmbeddedSkills, got %+v", p.EmbeddedSkills)
	}
}

// Denying the whole profile domain retires --profile: hidden from help,
// and setting it fails like an unknown flag.
func TestBuildInternal_profileFlagGate(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"profile", "profile/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	fl := root.PersistentFlags().Lookup("profile")
	if fl == nil {
		t.Fatal("--profile not registered")
	}
	if !fl.Hidden || !isPolicyGatedFlag(fl) {
		t.Errorf("flag should be hidden and policy-gated; hidden=%v gated=%v", fl.Hidden, isPolicyGatedFlag(fl))
	}

	// The gate rejects at the Value level: cobra's parse (help/version
	// fast paths included) can never set a gated flag.
	err := root.PersistentFlags().Set("profile", "prod")
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --profile") {
		t.Errorf("setting a gated flag must fail as unknown flag at parse time, got %v", err)
	}
}

// The gate must also fire on the real dispatch path. cobra parses a persistent
// flag on the leaf command's merged flagset, so a gate that walks
// root.PersistentFlags()'s changed table is a no-op once the flag is passed to
// a subcommand. Drive root.Execute end to end and confirm the gated --profile
// is rejected before the command body runs.
func TestBuildInternal_profileFlagGate_RejectsOnDispatch(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"profile", "profile/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	ranBody := false
	root.AddCommand(&cobra.Command{
		Use:  "gateprobe",
		RunE: func(*cobra.Command, []string) error { ranBody = true; return nil },
	})

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--profile", "prod", "gateprobe"})

	err := root.Execute()
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Message, "unknown flag") {
		t.Errorf("a gated --profile passed on the dispatch path must fail as unknown flag, got %v", err)
	}
	if ranBody {
		t.Error("gated --profile must be rejected before the command body runs")
	}
}

// Without the profile domain denied, the gate stays inert.
func TestBuildInternal_profileFlagUntouchedWithoutDenial(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	if fl := root.PersistentFlags().Lookup("profile"); isPolicyGatedFlag(fl) {
		t.Error("--profile must not be gated when its domain is not denied")
	}
}

// Denying the skills domain drops the root-help skills-setup footer.
func TestBuildInternal_skillsFooterSuppressed(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"skills", "skills/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	if strings.Contains(root.UsageTemplate(), "Skills setup") {
		t.Error("skills-setup footer must be dropped when the skills domain is denied")
	}

	// Control: without the denial the footer stays.
	platform.ResetForTesting()
	policystate.ResetForTesting()
	_, root2, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	if !strings.Contains(root2.UsageTemplate(), "Skills setup") {
		t.Error("skills-setup footer must stay in the default build")
	}
}

// Denying the skills command domain kills every `skills read` pointer in
// domain help, even when the skill content itself is still embedded — the
// command the pointers name is absent, so they would all be dead ends.
func TestBuildInternal_skillsDomainDenyKillsHelpPointers(t *testing.T) {
	tmpHome(t)
	withBaseSkills(t, map[string]string{"lark-im/SKILL.md": "---\ndescription: im\n---\n"})
	restrictingPlugin(t, []string{"skills", "skills/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	im := findByPath(root, "im")
	if im == nil {
		t.Fatal("im domain not in tree")
	}
	var buf bytes.Buffer
	im.SetOut(&buf)
	im.SetErr(&buf)
	root.HelpFunc()(im, nil)
	if strings.Contains(buf.String(), "skills read") {
		t.Errorf("domain help must not point at the denied skills command, got:\n%s", buf.String())
	}
}

// Denying the update domain silences the _notice providers that would
// steer the caller to `lark-cli update`.
func TestComposePendingNotice_updateDomainDenied(t *testing.T) {
	update.SetPending(&update.UpdateInfo{Current: "1.0.0", Latest: "1.0.1"})
	t.Cleanup(func() { update.SetPending(nil) })
	t.Cleanup(policystate.ResetForTesting)

	policystate.SetPluginDeniedDomains(map[string]bool{"update": true})
	if got := composePendingNotice(); got != nil {
		t.Errorf("notices must be silenced with the update domain denied, got %+v", got)
	}

	policystate.SetPluginDeniedDomains(nil)
	if got := composePendingNotice(); got == nil {
		t.Error("notices must render without the denial")
	}
}

// HideDiagnostics retires the exemptions — and with them gone, nothing keeps
// the config group alive: it must converge exactly like any fully-denied
// domain (absent from help, bare invocation and explicit help both answer
// command_unavailable). Guards the group level, which the leaf-only test
// below does not.
func TestBuildInternal_hideDiagnosticsConvergesDomainGroup(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, func(b *platform.Builder) *platform.Builder {
		return b.HideDiagnostics()
	})

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	cfg := findByPath(root, "config")
	if cfg == nil {
		t.Fatal("config group not in tree")
	}
	if cfg.IsAvailableCommand() {
		t.Error("config group must leave help/completion when HideDiagnostics retires its last live descendants")
	}

	if cfg.RunE == nil {
		t.Fatal("converged config group must carry an unavailable stub RunE")
	}
	err := cfg.RunE(cfg, nil)
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("bare `config` must answer command_unavailable, got %v", err)
	}

	var buf bytes.Buffer
	cfg.SetOut(&buf)
	cfg.SetErr(&buf)
	root.HelpFunc()(cfg, nil)
	out := buf.String()
	if !strings.Contains(out, "command_unavailable") {
		t.Errorf("`config --help` must answer command_unavailable, got:\n%.200s", out)
	}
	if strings.Contains(out, "Usage:") {
		t.Errorf("`config --help` must not render the original usage, got:\n%.200s", out)
	}

	// The converged domain must also reach the render-time snapshot, so
	// fixed hints (e.g. the strict-mode stub's `config strict-mode`
	// pointer) stop pointing at a domain this build presents as absent.
	if !policystate.DomainDeniedByPlugin("config") {
		t.Error("converged config domain must be recorded for render-time hint gating")
	}
}

// Default: diagnostics stay executable but leave help when their whole
// domain is denied — cobra then drops the empty config group entirely.
func TestBuildInternal_concealDiagnostics(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	show := findByPath(root, "config/policy/show")
	if show == nil {
		t.Fatal("config policy show not in tree")
	}
	if !show.Hidden {
		t.Error("exempt diagnostic should be hidden from help when its domain is denied")
	}
	// Still dispatchable: its RunE is the original, not an unavailable stub.
	if show.Annotations[cmdpolicy.AnnotationDenialLayer] != "" {
		t.Error("exempt diagnostic must not carry a denial stub by default")
	}
	if cfg := findByPath(root, "config"); cfg.IsAvailableCommand() {
		t.Error("config group with no visible children must drop from help")
	}
}

// Strict-mode discovery resolves credentials before plugin policy is applied
// and caches the not-configured error. The final envelope guard must remove its
// now-dead config-init hint after presentation converges the config domain.
func TestBuildInternal_cachedNotConfiguredHintFollowsPluginPresentation(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config", "config/**"}, nil)

	var stdout, stderr bytes.Buffer
	f, _, _ := buildInternal(context.Background(), buildInvocationForTest(t),
		WithIO(strings.NewReader(""), &stdout, &stderr))
	_, err := f.Config()
	p, ok := errs.ProblemOf(err)
	if !ok || p.Subtype != errs.SubtypeNotConfigured {
		t.Fatalf("Config() error = %T %v, want typed not_configured", err, err)
	}
	if p.Hint == "" {
		t.Fatal("test precondition failed: early cached error has no config hint")
	}
	if !policystate.DomainDeniedByPlugin("config") {
		t.Fatal("test precondition failed: config domain did not converge")
	}

	if code := handleRootError(f, err); code != 3 {
		t.Fatalf("handleRootError() exit = %d, want 3", code)
	}
	if p.Hint != "" {
		t.Fatalf("final recovery hint = %q, want empty for retired config domain", p.Hint)
	}
	if out := stderr.String(); !strings.Contains(out, `"subtype": "not_configured"`) || strings.Contains(out, "config init") {
		t.Fatalf("final envelope did not apply recovery-hint gate:\n%s", out)
	}
}

func TestApplyPolicyRecoveryHintGate_preservesUnrelatedAndRawHints(t *testing.T) {
	policystate.ResetForTesting()
	t.Cleanup(policystate.ResetForTesting)
	previousWorkspace := core.CurrentWorkspace()
	core.SetCurrentWorkspace(core.WorkspaceLocal)
	t.Cleanup(func() { core.SetCurrentWorkspace(previousWorkspace) })

	canonical := core.NotConfiguredError()
	raw := errs.MarkRaw(core.NotConfiguredError())
	nonCanonical := errs.NewConfigError(errs.SubtypeNotConfigured, "profile missing").
		WithHint("available profiles: production")
	otherSubtype := errs.NewConfigError(errs.SubtypeInvalidConfig, "malformed").
		WithHint("repair the JSON file")
	otherCategory := errs.NewAuthenticationError(errs.SubtypeTokenMissing, "missing token").
		WithHint("use an external credential provider")

	policystate.SetPluginDeniedDomains(map[string]bool{"config": true})
	applyPolicyRecoveryHintGate(canonical)
	if p, _ := errs.ProblemOf(canonical); p.Hint != "" {
		t.Fatalf("canonical config-command hint = %q, want empty", p.Hint)
	}
	for name, err := range map[string]error{
		"raw":            raw,
		"non-canonical":  nonCanonical,
		"other subtype":  otherSubtype,
		"other category": otherCategory,
	} {
		p, ok := errs.ProblemOf(err)
		if !ok || p.Hint == "" {
			t.Fatalf("%s test precondition failed: %+v", name, err)
		}
		want := p.Hint
		applyPolicyRecoveryHintGate(err)
		if p.Hint != want {
			t.Errorf("%s hint changed from %q to %q", name, want, p.Hint)
		}
	}

	policystate.SetPluginDeniedDomains(nil)
	withoutDenial := core.NotConfiguredError()
	p, _ := errs.ProblemOf(withoutDenial)
	want := p.Hint
	applyPolicyRecoveryHintGate(withoutDenial)
	if p.Hint != want {
		t.Fatalf("canonical hint changed without config denial: %q -> %q", want, p.Hint)
	}
}

// HideDiagnostics retires the exemptions like any other denied command.
func TestBuildInternal_hideDiagnostics(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, func(b *platform.Builder) *platform.Builder {
		return b.HideDiagnostics()
	})

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	show := findByPath(root, "config/policy/show")
	if show == nil {
		t.Fatal("config policy show not in tree")
	}
	err := show.RunE(show, nil)
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("hidden diagnostic must answer command_unavailable, got %v", err)
	}
}

// A plugin Wrapper that swallows errors (returns nil without calling next)
// must not defeat the help meta command's unavailable interception: `help
// <restricted>` is framework presentation, not business dispatch, so it stays
// outside the Wrap chain and keeps answering command_unavailable.
func TestBuildInternal_helpCommandImmuneToWrapperSwallow(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, func(b *platform.Builder) *platform.Builder {
		return b.Wrap("swallow", platform.All(), func(platform.Handler) platform.Handler {
			return func(context.Context, platform.Invocation) error { return nil }
		})
	})

	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))
	helpCmd := findByPath(root, "help")
	if helpCmd == nil || helpCmd.RunE == nil {
		t.Fatal("custom help command not installed")
	}

	err := helpCmd.RunE(helpCmd, []string{"config"})
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
		t.Errorf("help on a restricted command must answer command_unavailable even under a swallowing Wrapper, got %v", err)
	}
}

// A gated --profile must be rejected at parse time, so cobra's help/version
// fast paths (which never reach PersistentPreRunE) cannot slip past the gate.
func TestBuildInternal_gatedProfileRejectedOnHelpFastPath(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"profile", "profile/**"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	for _, args := range [][]string{
		{"--profile", "prod", "--help"},
		{"--profile", "prod", "--version"},
		{"--profile", "prod", "skills", "--help"},
	} {
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetErr(&buf)
		root.SetArgs(args)
		err := root.Execute()
		var ve *errs.ValidationError
		if !errors.As(err, &ve) || !strings.Contains(ve.Message, `unknown flag "--profile"`) {
			t.Errorf("%v: gated --profile must fail as unknown flag on the fast path, got %v", args, err)
		}
	}

	// Un-gated flags parse normally (no over-rejection).
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("bare --help must still work, got %v", err)
	}
}

// With HideDiagnostics, every group on the exemption chain answers
// unavailable — bare `config policy` / `config plugins` included, not only
// the show leaves.
func TestBuildInternal_retireDiagnosticsCoversIntermediates(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, func(b *platform.Builder) *platform.Builder {
		return b.HideDiagnostics()
	})
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	for _, path := range []string{"config/policy", "config/plugins", "config/policy/show", "config/plugins/show"} {
		c := findByPath(root, path)
		if c == nil || c.RunE == nil {
			t.Fatalf("%s missing or without RunE", path)
		}
		err := c.RunE(c, nil)
		var ve *errs.ValidationError
		if !errors.As(err, &ve) || ve.Subtype != errs.SubtypeCommandUnavailable {
			t.Errorf("%s must answer command_unavailable, got %v", path, err)
		}
	}
}

// Rule.DeniedMessage reaches the synthesized diagnostics denial even when
// the rule itself denies no command.
func TestBuildInternal_deniedMessageAppliesToRetiredDiagnostics(t *testing.T) {
	tmpHome(t)
	platform.ResetForTesting()
	t.Cleanup(platform.ResetForTesting)
	t.Cleanup(func() { policystate.ResetForTesting() })
	platform.Register(platform.NewPlugin("acme", "1.0").
		Restrict(&platform.Rule{Deny: []string{"zz-not-a-cmd/**"}, DeniedMessage: "not part of acme cli"}).
		HideDiagnostics().
		MustBuild())
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	c := findByPath(root, "config/policy/show")
	err := c.RunE(c, nil)
	var ve *errs.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected validation error, got %T %v", err, err)
	}
	if ve.Message != "not part of acme cli" {
		t.Errorf("retired diagnostic message = %q, want the rule's DeniedMessage", ve.Message)
	}
}

// The custom help command keeps stock cobra fidelity: subcommand completion
// works (denied commands, being hidden, never appear), and its own help
// carries no Risk line the stock command lacks.
func TestBuildInternal_helpCompletionMatchesStock(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	h := findByPath(root, "help")
	if h.ValidArgsFunction == nil {
		t.Fatal("help must offer subcommand completion like stock cobra")
	}
	comps, _ := h.ValidArgsFunction(h, nil, "")
	joined := strings.Join(comps, "\n")
	if !strings.Contains(joined, "im\t") {
		t.Errorf("help completion must offer live commands, got:\n%s", joined)
	}
	if strings.Contains(joined, "config\t") {
		t.Errorf("help completion must not offer a denied domain, got:\n%s", joined)
	}

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"help", "help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help help: %v", err)
	}
	if strings.Contains(buf.String(), "Risk:") {
		t.Errorf("help's own help must not carry a Risk line, got:\n%s", buf.String())
	}
}

// A plugin-denied command's local flags leave shell completion: the command
// presents as absent, so its flag surface must not be enumerable.
func TestBuildInternal_deniedCommandFlagsLeaveCompletion(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"skills/read"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"__complete", "skills", "read", "--"})
	_ = root.Execute()
	if strings.Contains(buf.String(), "--json") {
		t.Errorf("denied command's local flags must not complete, got:\n%s", buf.String())
	}
}

// Presentation-time denials (retired diagnostics, converged groups) reach the
// recorded ActivePolicy, so `config policy show` reflects the shipped tree.
func TestBuildInternal_activePolicyIncludesRetiredDiagnostics(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"config/**"}, func(b *platform.Builder) *platform.Builder {
		return b.HideDiagnostics()
	})
	buildInternal(context.Background(), buildInvocationForTest(t))

	ap := cmdpolicy.GetActive()
	if ap == nil {
		t.Fatal("no active policy recorded")
	}
	for _, path := range []string{"config", "config/policy", "config/policy/show", "config/plugins", "config/plugins/show"} {
		if _, ok := ap.DeniedByPath[path]; !ok {
			t.Errorf("ActivePolicy.DeniedByPath missing %q (presentation-time denial not recorded)", path)
		}
	}
}

// A bare gated --profile (no value) presents as unknown, same as a set one:
// pflag's "needs an argument" path must not leak a different shape.
func TestBuildInternal_gatedProfileBareFlagPresentsUnknown(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"profile", "profile/**"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--profile"})
	err := root.Execute()
	var ve *errs.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Message, `unknown flag "--profile"`) {
		t.Errorf("bare gated --profile must present as unknown flag, got %v", err)
	}
}

// Execute's bootstrap parser runs before plugin installation. It must defer a
// missing-value error to Cobra, otherwise a bare retired --profile escapes as
// plain text before the policy gate exists.
func TestExecute_profileBootstrapErrorsDeferToCobra(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_NO_UPDATE_NOTIFIER", "1")
	t.Setenv("LARKSUITE_CLI_NO_SKILLS_NOTIFIER", "1")

	t.Run("retired flag is unknown", func(t *testing.T) {
		tmpHome(t)
		platform.ResetForTesting()
		policystate.ResetForTesting()
		t.Cleanup(platform.ResetForTesting)
		t.Cleanup(policystate.ResetForTesting)
		platform.Register(platform.NewPlugin("acme", "1.0").
			Restrict(&platform.Rule{Deny: []string{"profile", "profile/**"}}).
			MustBuild())

		code, _, stderr := executeWithCapturedOS(t, "--profile")
		if code != 2 || !strings.Contains(stderr, `"subtype": "invalid_argument"`) ||
			!strings.Contains(stderr, `unknown flag \"--profile\"`) {
			t.Fatalf("retired bare --profile: exit=%d stderr=%s", code, stderr)
		}
	})

	t.Run("ordinary flag keeps needs-argument error", func(t *testing.T) {
		tmpHome(t)
		platform.ResetForTesting()
		policystate.ResetForTesting()
		t.Cleanup(platform.ResetForTesting)
		t.Cleanup(policystate.ResetForTesting)

		code, _, stderr := executeWithCapturedOS(t, "--profile")
		if code != 2 || !strings.Contains(stderr, "flag needs an argument") || strings.Contains(stderr, "unknown flag") {
			t.Fatalf("ordinary bare --profile: exit=%d stderr=%s", code, stderr)
		}
	})

	t.Run("valid profile still reaches help", func(t *testing.T) {
		tmpHome(t)
		platform.ResetForTesting()
		policystate.ResetForTesting()
		t.Cleanup(platform.ResetForTesting)
		t.Cleanup(policystate.ResetForTesting)

		code, stdout, stderr := executeWithCapturedOS(t, "--profile", "prod", "--help")
		if code != 0 || !strings.Contains(stdout, "Usage:") {
			t.Fatalf("valid profile help: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})
}

func executeWithCapturedOS(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	oldArgs, oldStdout, oldStderr := os.Args, os.Stdout, os.Stderr
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		os.Args, os.Stdout, os.Stderr = oldArgs, oldStdout, oldStderr
	}
	defer restore()
	os.Args = append([]string{"e2e-cli"}, args...)
	os.Stdout, os.Stderr = stdout, stderr
	code := Execute()
	restore()
	if err := stdout.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stderr.Close(); err != nil {
		t.Fatal(err)
	}
	stdoutData, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	stderrData, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(stdoutData), string(stderrData)
}

// A plugin-denied command offers no positional completion either.
func TestBuildInternal_deniedCommandPositionalsLeaveCompletion(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"skills/read"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"__complete", "skills", "read", ""})
	_ = root.Execute()
	if strings.Contains(buf.String(), "lark-") {
		t.Errorf("denied command must not complete positional args, got:\n%s", buf.String())
	}
}

// A denied command that offers static ValidArgs (completion's shell names)
// must not complete them either: cobra serves ValidArgs through a separate
// channel from ValidArgsFunction, and both leave with the denial.
func TestBuildInternal_deniedCommandStaticValidArgsLeaveCompletion(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, []string{"completion", "completion/**"}, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"__complete", "completion", ""})
	_ = root.Execute()
	if strings.Contains(buf.String(), "bash") {
		t.Errorf("denied completion command must not offer shell names, got:\n%s", buf.String())
	}
}

// A fresh `lark-cli help` must render root usage with --version, exactly like
// stock cobra (whose help command inits the version flag before rendering).
// Fresh root + single invocation: an earlier --help run would register the
// flag as a side effect and mask the regression.
func TestBuildInternal_helpShowsVersionFlagFresh(t *testing.T) {
	tmpHome(t)
	restrictingPlugin(t, nil, nil)
	_, root, _ := buildInternal(context.Background(), buildInvocationForTest(t))

	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if !strings.Contains(buf.String(), "--version") {
		t.Errorf("fresh `help` must show --version like stock cobra, got:\n%.300s", buf.String())
	}
}

// spec'd precedence: a command restricted by BOTH the user's strict-mode and a
// plugin Rule keeps the strict-mode identity error — strict-mode is a
// user-side security boundary and is never re-labelled as absent.
func TestApply_strictStubWinsOverPluginDenial(t *testing.T) {
	root := newTestTree()
	pruneForStrictMode(root, core.StrictModeBot)
	stub := findCmd(root, "auth", "login")
	if stub == nil {
		t.Fatal("auth/login strict stub missing")
	}

	n := cmdpolicy.Apply(root, map[string]cmdpolicy.Denial{
		"auth/login": {Layer: cmdpolicy.LayerPolicy, PolicySource: "plugin:acme"},
	})
	_ = n

	if got := stub.Annotations[cmdpolicy.AnnotationDenialLayer]; got != cmdpolicy.LayerStrictMode {
		t.Fatalf("denial layer = %q, want strict_mode preserved", got)
	}
	err := stub.RunE(stub, nil)
	if err == nil || !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("double-restricted command must keep the strict-mode error, got %v", err)
	}
}

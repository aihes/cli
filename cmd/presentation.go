// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/larksuite/cli/internal/cmdpolicy"
	internalplatform "github.com/larksuite/cli/internal/platform"
	"github.com/larksuite/cli/internal/policystate"
)

// applyPluginPresentation makes plugin-denied capabilities present as
// absent: retired flags, no skills footer, diagnostics hidden or retired.
// Presentation only — enforcement happened in cmdpolicy.Apply.
func applyPluginPresentation(rootCmd *cobra.Command, installResult *internalplatform.InstallResult, denied map[string]cmdpolicy.Denial) {
	applyPluginFlagGate(rootCmd, denied)

	if domainDeniedByPlugin(denied, "skills") {
		rootCmd.SetUsageTemplate(strings.Replace(rootUsageTemplate, skillsSetupFooter, "", 1))
	}

	if installResult != nil && hideDiagnosticsOwner(installResult.Plugins) != "" {
		retireDiagnostics(rootCmd, installResult, denied)
	}
	// Group convergence runs on both paths: with the exemptions retired
	// (or merely concealed), nothing keeps their domain group alive, and
	// it must present as absent like any fully-denied domain.
	concealDiagnostics(rootCmd, denied)
}

// domainDeniedByPlugin reads the freshly-built denied map, unlike
// policystate.DomainDeniedByPlugin which serves render-time consumers.
func domainDeniedByPlugin(denied map[string]cmdpolicy.Denial, domain string) bool {
	d, ok := denied[domain]
	return ok && cmdpolicy.IsPluginPolicySource(d.PolicySource)
}

// hideDiagnosticsOwner returns the plugin that declared HideDiagnostics,
// or "". The host already enforced that it also Restricts.
func hideDiagnosticsOwner(plugins []internalplatform.PluginInfo) string {
	for _, p := range plugins {
		if p.Capabilities.HideDiagnostics {
			return p.Name
		}
	}
	return ""
}

// retireDiagnostics installs the unavailable presentation on the
// diagnostic exemptions, same as any other plugin-denied command.
func retireDiagnostics(rootCmd *cobra.Command, installResult *internalplatform.InstallResult, denied map[string]cmdpolicy.Denial) {
	source := "plugin:" + hideDiagnosticsOwner(installResult.Plugins)
	message := ""
	for _, d := range denied {
		if cmdpolicy.IsPluginPolicySource(d.PolicySource) && d.DeniedMessage != "" {
			message = d.DeniedMessage
			break
		}
	}
	// A rule can declare DeniedMessage yet deny no command of its own;
	// the synthesized diagnostics denial still speaks in its voice.
	if message == "" {
		if ap := cmdpolicy.GetActive(); ap != nil && ap.Source.Kind == cmdpolicy.SourcePlugin {
			for _, r := range ap.Rules {
				if r.DeniedMessage != "" {
					message = r.DeniedMessage
					break
				}
			}
		}
	}
	diag := map[string]cmdpolicy.Denial{}
	for _, path := range cmdpolicy.DiagnosticPaths() {
		// The whole exemption chain retires: the leaf and every group
		// between it and the top-level domain answer unavailable, not
		// bare help at exit 0.
		for p := path; strings.Contains(p, "/"); p = p[:strings.LastIndex(p, "/")] {
			diag[p] = cmdpolicy.Denial{
				Layer:         cmdpolicy.LayerPolicy,
				PolicySource:  source,
				ReasonCode:    "diagnostics_hidden",
				Reason:        "policy self-inspection hidden by the integrator",
				DeniedMessage: message,
			}
		}
	}
	cmdpolicy.Apply(rootCmd, diag)
	cmdpolicy.AppendActiveDenials(diag)
}

// concealDiagnostics hides the diagnostic exemptions from help — without
// touching their RunE, so they stay dispatchable — once every non-exempt
// sibling in their domain is already hidden. The group itself then gets
// the unavailable stub: it escaped denial aggregation only because the
// exemptions kept a runnable descendant alive, and its unknown-subcommand
// guard RunE would otherwise keep it listed in help.
func concealDiagnostics(rootCmd *cobra.Command, denied map[string]cmdpolicy.Denial) {
	for _, group := range diagnosticDomainGroups(rootCmd) {
		if !pluginDeniedUnder(denied, cmdpolicy.CanonicalPath(group)) {
			continue
		}
		exemptAncestors := map[*cobra.Command]bool{}
		for _, path := range cmdpolicy.DiagnosticPaths() {
			for c := findByPath(rootCmd, path); c != nil && c != group; c = c.Parent() {
				exemptAncestors[c] = true
			}
		}
		allOthersHidden := true
		for _, child := range group.Commands() {
			if child.Name() == "help" || exemptAncestors[child] {
				continue
			}
			if !child.Hidden {
				allOthersHidden = false
				break
			}
		}
		if !allOthersHidden {
			continue
		}
		for c := range exemptAncestors {
			c.Hidden = true
		}
		var sample cmdpolicy.Denial
		for path, d := range denied {
			if strings.HasPrefix(path, cmdpolicy.CanonicalPath(group)+"/") && cmdpolicy.IsPluginPolicySource(d.PolicySource) {
				sample = d
				break
			}
		}
		groupDenial := cmdpolicy.Denial{
			Layer:         cmdpolicy.LayerPolicy,
			PolicySource:  sample.PolicySource,
			RuleName:      sample.RuleName,
			ReasonCode:    "all_children_denied",
			Reason:        "all child commands are denied",
			DeniedMessage: sample.DeniedMessage,
		}
		cmdpolicy.Apply(rootCmd, map[string]cmdpolicy.Denial{
			cmdpolicy.CanonicalPath(group): groupDenial,
		})
		// The bootstrap snapshot could not see this whole-domain denial
		// (the exemptions were still alive then); record it now so
		// render-time hints and policy introspection stop pointing into
		// the converged domain.
		policystate.AddPluginDeniedDomain(cmdpolicy.CanonicalPath(group))
		cmdpolicy.AppendActiveDenials(map[string]cmdpolicy.Denial{
			cmdpolicy.CanonicalPath(group): groupDenial,
		})
	}
}

// diagnosticDomainGroups returns the top-level groups containing a
// diagnostic exemption (today just `config`).
func diagnosticDomainGroups(rootCmd *cobra.Command) []*cobra.Command {
	seen := map[*cobra.Command]bool{}
	var out []*cobra.Command
	for _, path := range cmdpolicy.DiagnosticPaths() {
		top, _, _ := strings.Cut(path, "/")
		if c := findByPath(rootCmd, top); c != nil && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func pluginDeniedUnder(denied map[string]cmdpolicy.Denial, prefix string) bool {
	for path, d := range denied {
		if strings.HasPrefix(path, prefix+"/") && cmdpolicy.IsPluginPolicySource(d.PolicySource) {
			return true
		}
	}
	return false
}

// findByPath resolves a canonical slash path (e.g. "config/policy/show")
// to the command node, or nil.
func findByPath(rootCmd *cobra.Command, path string) *cobra.Command {
	cur := rootCmd
	for _, seg := range strings.Split(path, "/") {
		var next *cobra.Command
		for _, child := range cur.Commands() {
			if child.Name() == seg {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

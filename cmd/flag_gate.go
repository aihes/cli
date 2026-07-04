// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package cmd

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/larksuite/cli/internal/cmdpolicy"
)

// globalFlagDomains maps each root persistent flag to the command domain
// it belongs to. A new domain-tied global flag must add a row.
var globalFlagDomains = map[string]string{
	"profile": "profile",
}

// flagGateAnnotation distinguishes a policy-retired flag from one hidden
// cosmetically (single-app mode force-shows the latter in root help).
const flagGateAnnotation = "lark:policy_denied_flag"

// applyPluginFlagGate hides and rejects the global flags whose whole
// domain a plugin denied. yaml denials do not gate flags. Must run after
// RegisterGlobalFlags and cmdpolicy.Apply.
func applyPluginFlagGate(root *cobra.Command, denied map[string]cmdpolicy.Denial) {
	for flagName, domain := range globalFlagDomains {
		d, ok := denied[domain]
		if !ok || !cmdpolicy.IsPluginPolicySource(d.PolicySource) {
			continue
		}
		fl := root.PersistentFlags().Lookup(flagName)
		if fl == nil {
			continue
		}
		fl.Hidden = true
		if fl.Annotations == nil {
			fl.Annotations = map[string][]string{}
		}
		fl.Annotations[flagGateAnnotation] = []string{"true"}
		fl.Value = &gatedFlagValue{name: flagName, inner: fl.Value}
	}
}

func isPolicyGatedFlag(fl *pflag.Flag) bool {
	return fl != nil && fl.Annotations[flagGateAnnotation] != nil
}

// gatedFlagValue rejects at parse time, before cobra's help/version fast
// paths (which never reach PersistentPreRunE). Its Set error carries
// cobra's own unknown-flag wording so the root FlagErrorFunc renders the
// same envelope an unregistered flag produces.
type gatedFlagValue struct {
	name  string
	inner pflag.Value
}

func (g *gatedFlagValue) String() string { return g.inner.String() }
func (g *gatedFlagValue) Type() string   { return g.inner.Type() }
func (g *gatedFlagValue) Set(string) error {
	// Intermediate parse error, not a final envelope: pflag wraps it and
	// the root FlagErrorFunc (flagDidYouMean) converts it to the typed
	// unknown-flag validation error.
	return errors.New("unknown flag: --" + g.name) //nolint:forbidigo // intermediate parse error; flagDidYouMean emits the typed envelope
}

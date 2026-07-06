// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package credential

// CredentialSourceKind is the wire-stable App/credential selection source.
type CredentialSourceKind string

const (
	SourceFlagProfile      CredentialSourceKind = "flag:--profile"
	SourceEnvProfile       CredentialSourceKind = "env:LARKSUITE_CLI_PROFILE"
	SourceEnvAppID         CredentialSourceKind = "env:LARKSUITE_CLI_APP_ID"
	SourceConfigCurrentApp CredentialSourceKind = "config:currentApp"
	SourceConfigFirstApp   CredentialSourceKind = "config:firstApp"
)

// DirectCredentialEnv describes the state of direct app credential env vars.
// It never carries a secret value — only names and the non-sensitive app_id.
type DirectCredentialEnv struct {
	Present              bool     `json:"present"`
	Keys                 []string `json:"keys,omitempty"`
	AppID                string   `json:"appId,omitempty"`
	Matched              bool     `json:"matched,omitempty"`
	ConflictsWithProfile bool     `json:"conflictsWithProfile,omitempty"`
}

// IdentitySelection is the explainable result of credential selection.
// It carries NO secret value (security: §5.1).
type IdentitySelection struct {
	Source              CredentialSourceKind
	DirectCredentialEnv DirectCredentialEnv
}

// Explicit reports whether the identity was actively specified by the
// user/agent (flag or env), which governs no-fallback behavior.
func (s IdentitySelection) Explicit() bool {
	switch s.Source {
	case SourceFlagProfile, SourceEnvProfile, SourceEnvAppID:
		return true
	default:
		return false
	}
}

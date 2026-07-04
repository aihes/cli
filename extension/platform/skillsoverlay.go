// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package platform

import "io/fs"

// SkillsOverlay declares how a plugin customizes the CLI's embedded
// skill content, contributed via Builder.EmbeddedSkills. At most one
// source may own skill content; two customizing plugins abort startup.
//
// Allow / Remove mirror Rule's Allow / Deny: an allow-list keeps only
// what it names, a remove-list drops what it names, and Remove wins
// over Allow. Composition order is fixed: Base (or the CLI default) ->
// Allow -> Remove -> Overlay, a same-named skill resolving to Overlay.
//
// Skills are addressed by exact name (a directory carrying SKILL.md,
// e.g. "lark-doc"), not by command path and not by glob — the skill
// list is flat, so misspellings abort startup instead of silently
// matching nothing. Removing a skill only drops its guidance; it does
// not disable any command (use Restrict for that).
//
// The top-level skill set and each skill's owning FS are snapshotted when
// the CLI builds. Later additions or removals of top-level directories do
// not change the manifest; files within an owned skill directory are read
// live. Base and Overlay must contain only valid skill directories.
type SkillsOverlay struct {
	// Allow, when non-empty, keeps only these skills (by name) from the
	// base tree — the allow-list counterpart of Rule.Allow. Skills the
	// CLI adds in future versions stay out of the build until listed
	// here, which a Remove-only spec cannot guarantee. A name not
	// present in the base aborts startup. Overlay entries are exempt:
	// content the integrator explicitly ships needs no allow-listing.
	Allow []string

	// Remove hides these skills, by name (e.g. "lark-shared"), from the
	// base tree; it wins over Allow, mirroring Rule's Deny-over-Allow. A
	// name not present in the base aborts startup rather than being
	// silently ignored.
	Remove []string

	// Overlay contributes skills laid over the base: a same-named skill
	// replaces the base's entirely, a new name adds one. It is rooted at
	// the skill list (entries like "my-skill/SKILL.md"); each top-level
	// entry must be a "<name>/" directory containing SKILL.md. Any fs.FS
	// works (embed.FS, os.DirFS, fstest.MapFS); embed.FS is not required.
	Overlay fs.FS

	// Base replaces the entire base skill tree instead of layering over
	// the CLI default. nil keeps the CLI default. Every top-level entry
	// must be a valid skill directory containing SKILL.md. Rare: most
	// integrators leave it nil and use Remove/Overlay so unchanged skills
	// follow the CLI version with no copy to maintain.
	Base fs.FS
}

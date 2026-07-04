// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package skillpolicy composes the CLI's effective embedded skill tree
// from a base skill FS and at most one plugin-supplied SkillsOverlay. It is
// the skill-side analogue of internal/cmdpolicy: plugins contribute a
// delta over a base, and one resolver produces the single tree that both
// skill readers consume -- `skills list`/`read` and the --help
// domain-guide pointer.
package skillpolicy

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/larksuite/cli/extension/platform"
)

// PluginSkill pairs a plugin name with the SkillsOverlay it contributed, so a
// conflict can be attributed to specific owners. Mirrors
// cmdpolicy.PluginRule.
type PluginSkill struct {
	PluginName    string
	SkillsOverlay *platform.SkillsOverlay
}

// ErrMultipleSkillsOverlays reports that more than one plugin tried to
// customize skill content. Mirrors cmdpolicy.ErrMultipleRestricts: only
// one owner is allowed so independent plugins cannot silently overwrite
// each other's skill tree.
var ErrMultipleSkillsOverlays = errors.New("multiple plugins customized skills; only one plugin may own skill content")

// Resolve composes the effective skill tree from base and the supplied
// specs. base is the CLI's embedded skill FS (nil when the build embeds
// none). With no spec it returns base unchanged. With exactly one spec it
// applies, in fixed order, Base override -> Allow -> Remove -> Overlay,
// returning an overlay FS in which a same-named skill resolves to
// Overlay. Two or more distinct owners is a configuration error.
func Resolve(base fs.FS, specs []PluginSkill) (fs.FS, error) {
	owners := distinctOwners(specs)
	if len(owners) > 1 {
		return nil, fmt.Errorf("%w: %v", ErrMultipleSkillsOverlays, owners)
	}
	if len(specs) == 0 || specs[0].SkillsOverlay == nil {
		return base, nil
	}

	owner, spec := specs[0].PluginName, specs[0].SkillsOverlay
	lower := base
	if spec.Base != nil {
		lower = spec.Base
	}
	lowerSnapshot, err := scanSkillTree("Base", lower)
	if err != nil {
		return nil, fmt.Errorf("plugin %q skill spec: %w", owner, err)
	}
	upperSnapshot, err := scanSkillTree("Overlay", spec.Overlay)
	if err != nil {
		return nil, fmt.Errorf("plugin %q skill spec: %w", owner, err)
	}
	if err := validateSelection(lowerSnapshot, spec); err != nil {
		return nil, fmt.Errorf("plugin %q skill spec: %w", owner, err)
	}
	if lower == nil && spec.Overlay == nil {
		return nil, nil
	}
	return newOverlayFS(lowerSnapshot, upperSnapshot, spec.Remove, spec.Allow), nil
}

// distinctOwners returns the unique contributing plugin names in
// first-seen order. Mirrors cmdpolicy.distinctOwners.
func distinctOwners(specs []PluginSkill) []string {
	seen := map[string]bool{}
	owners := make([]string, 0, len(specs))
	for _, s := range specs {
		if !seen[s.PluginName] {
			seen[s.PluginName] = true
			owners = append(owners, s.PluginName)
		}
	}
	return owners
}

type skillTreeSnapshot struct {
	source  fs.FS
	entries []fs.DirEntry
	names   map[string]bool
}

// scanSkillTree validates and snapshots a skill tree's top level in one
// pass. The returned entries are the exact entries used by the overlay
// manifest, so a mutable FS cannot swap unvalidated names in between
// validation and composition.
func scanSkillTree(label string, source fs.FS) (skillTreeSnapshot, error) {
	snapshot := skillTreeSnapshot{source: source, names: map[string]bool{}}
	if source == nil {
		return snapshot, nil
	}
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return snapshot, fmt.Errorf("%s: cannot read root: %w", label, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			return snapshot, fmt.Errorf("%s: %q is not a directory; every %s entry must be a <skill>/ dir", label, e.Name(), label)
		}
		if !isSkillName(e.Name()) {
			return snapshot, fmt.Errorf("%s: %q is not a valid skill name", label, e.Name())
		}
		ok, err := skillExists(source, e.Name())
		if err != nil {
			return snapshot, fmt.Errorf("%s: probing skill %q: %w", label, e.Name(), err)
		}
		if !ok {
			return snapshot, fmt.Errorf("%s: skill %q is missing SKILL.md", label, e.Name())
		}
		snapshot.names[e.Name()] = true
	}
	snapshot.entries = entries
	return snapshot, nil
}

// validateSelection rejects allow/remove entries that cannot compose against
// the already-validated base snapshot.
func validateSelection(lower skillTreeSnapshot, spec *platform.SkillsOverlay) error {
	for _, name := range spec.Allow {
		if !isSkillName(name) {
			return fmt.Errorf("Allow: %q is not a valid skill name", name)
		}
		if !lower.names[name] {
			return fmt.Errorf("Allow: skill %q is not in the base tree", name)
		}
	}
	for _, name := range spec.Remove {
		if !isSkillName(name) {
			return fmt.Errorf("Remove: %q is not a valid skill name", name)
		}
		if !lower.names[name] {
			return fmt.Errorf("Remove: skill %q is not in the base tree", name)
		}
	}
	return nil
}

// skillExists reports whether fsys holds a skill named name -- a
// directory carrying SKILL.md, the shape internal/skillcontent treats as
// a skill.
func skillExists(fsys fs.FS, name string) (bool, error) {
	if fsys == nil {
		return false, nil
	}
	info, err := fs.Stat(fsys, name+"/SKILL.md")
	switch {
	case err == nil:
		return !info.IsDir(), nil
	case errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid):
		return false, nil
	default:
		// A permission or I/O fault is a real cause, not absence.
		return false, err
	}
}

// isSkillName rejects empty, dotted, or path-bearing names so Remove
// cannot smuggle a traversal or match outside the top level.
func isSkillName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

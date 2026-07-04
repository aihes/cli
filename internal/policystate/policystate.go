// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

// Package policystate answers "did an integrator plugin deny this whole
// command domain?" for render-time hint emitters. It is dependency-free
// because internal/auth and internal/client sit below internal/cmdpolicy
// in the import graph and cannot ask it directly. Written once by the
// bootstrap after policy pruning; read-only thereafter.
package policystate

import "sync"

var (
	mu                  sync.RWMutex
	pluginDeniedDomains map[string]bool
)

// SetPluginDeniedDomains records the plugin-denied top-level domains.
// nil clears.
func SetPluginDeniedDomains(domains map[string]bool) {
	mu.Lock()
	defer mu.Unlock()
	if domains == nil {
		pluginDeniedDomains = nil
		return
	}
	cp := make(map[string]bool, len(domains))
	for d, v := range domains {
		cp[d] = v
	}
	pluginDeniedDomains = cp
}

// AddPluginDeniedDomain records one more plugin-denied domain after the
// bootstrap snapshot: presentation-time convergence (a domain whose last
// live descendants were retired) discovers whole-domain denials the
// bootstrap aggregate cannot see.
func AddPluginDeniedDomain(domain string) {
	mu.Lock()
	defer mu.Unlock()
	if pluginDeniedDomains == nil {
		pluginDeniedDomains = map[string]bool{}
	}
	pluginDeniedDomains[domain] = true
}

// DomainDeniedByPlugin reports whether the whole top-level domain was
// denied by an integrator plugin. yaml denials never register here.
func DomainDeniedByPlugin(domain string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return pluginDeniedDomains[domain]
}

// ResetForTesting clears the recorded state.
func ResetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	pluginDeniedDomains = nil
}

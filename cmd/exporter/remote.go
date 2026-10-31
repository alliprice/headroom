package main

import (
	"context"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/provider"
)

// reconcileTimeout bounds one credential listing.
const reconcileTimeout = 30 * time.Second

// credentialLister is the part of cliproxy.Client that planning needs.
type credentialLister interface {
	Credentials(ctx context.Context) ([]cliproxy.Credential, error)
}

// remoteFactory builds the state for one proxy-held credential.
type remoteFactory func(cred cliproxy.Credential) *providerState

// remoteFactories lists the providers whose usage can be read from the
// proxy's record of their rate-limit headers. That record costs nothing
// upstream, so these states re-read it on every listing (cliproxy.relist)
// and keep it for cliproxy.max_age.
//
// Asking upstream instead does not work: CLIProxyAPI's Claude credentials
// are setup tokens without the user:profile scope, and /api/oauth/usage
// answers them 403.
func remoteFactories(cfg config) map[string]remoteFactory {
	relist, maxAge := cfg.CLIProxy.relistInterval(), cfg.CLIProxy.maxAge()
	return map[string]remoteFactory{
		"claude": func(cred cliproxy.Credential) *providerState {
			return newRemoteClaudeState(cred, relist, maxAge)
		},
	}
}

var errNoQuotaObserved = errors.New("no quota observed yet; CLIProxyAPI records it from the next request it sends with this credential")

// newRemoteClaudeState reads Session and Weekly from the
// anthropic-ratelimit-unified-* headers the proxy recorded. Their age is the
// record's observed_at, not the time of reading.
func newRemoteClaudeState(cred cliproxy.Credential, relist, maxAge time.Duration) *providerState {
	s := newRemoteState(provider.Provider{ID: "claude", DisplayName: "Claude"}, cred, relist, maxAge)
	s.prov.Fetch = func() (*provider.FetchResult, bool, error) {
		return remoteClaudeResult(s.currentCredential(), time.Now())
	}
	return s
}

func remoteClaudeResult(cred cliproxy.Credential, now time.Time) (*provider.FetchResult, bool, error) {
	categories := provider.ClaudeUnifiedCategories(cred.Quota.Signals, now)
	if len(categories) == 0 {
		return nil, false, errNoQuotaObserved
	}
	return &provider.FetchResult{Categories: categories, ObservedAt: cred.Quota.ObservedAt}, false, nil
}

// enableRemote turns on proxy-held account polling.
func (mc *multiCollector) enableRemote(proxy credentialLister, factories map[string]remoteFactory, relist time.Duration) {
	mc.proxy = proxy
	mc.factories = factories
	mc.relist = relist
	mc.reconcileCh = make(chan struct{}, 1)
}

// triggerReconcile asks for a listing soon, without waiting for one. With no
// proxy configured it does nothing.
func (mc *multiCollector) triggerReconcile() {
	select {
	case mc.reconcileCh <- struct{}{}:
	default:
	}
}

func (mc *multiCollector) runReconciler() {
	mc.reconcileNow()
	ticker := time.NewTicker(mc.relist)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
		case <-mc.reconcileCh:
		}
		mc.reconcileNow()
	}
}

func (mc *multiCollector) reconcileNow() {
	ctx, cancel := context.WithTimeout(context.Background(), reconcileTimeout)
	defer cancel()
	if err := mc.reconcile(ctx); err != nil {
		log.Printf("[cliproxy] listing failed, keeping %d remote accounts' last data: %v", mc.remoteCount(), err)
	}
}

func (mc *multiCollector) remoteCount() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	return len(mc.remote)
}

// reconcile lists the proxy's credentials, brings the remote states in line
// with them, and has each one re-read its credential's record. A failed
// listing leaves the set of states alone and marks each attempt failed: a
// state is removed only after a listing that succeeded leaves it out.
func (mc *multiCollector) reconcile(ctx context.Context) error {
	creds, err := mc.proxy.Credentials(ctx)
	if err != nil {
		_, remote := mc.states()
		for _, s := range remote {
			s.markFailed()
		}
		return err
	}

	local, _ := mc.states()
	localKeys := make(map[stateKey]bool, len(local))
	for _, s := range local {
		localKeys[s.key()] = true
	}

	mc.mu.Lock()
	polled := make(map[stateKey]cliproxy.Credential, len(mc.remote))
	for key, s := range mc.remote {
		polled[key] = s.currentCredential()
	}
	plan := planRemote(creds, localKeys, polled, func(id string) bool { return mc.factories[id] != nil })

	var removed, added []*providerState
	for key, s := range mc.remote {
		if want, ok := plan[key]; ok && sameCredential(want, polled[key]) {
			s.setCredential(want)
			continue
		}
		delete(mc.remote, key)
		removed = append(removed, s)
	}
	for key, cred := range plan {
		if _, ok := mc.remote[key]; ok {
			continue
		}
		s := mc.factories[key.provider](cred)
		mc.remote[key] = s
		added = append(added, s)
	}
	mc.mu.Unlock()

	byKey := func(states []*providerState) {
		sort.Slice(states, func(i, j int) bool { return states[i].key().String() < states[j].key().String() })
	}
	byKey(removed)
	byKey(added)
	for _, s := range removed {
		log.Printf("[%s] remote account removed", s.logName())
	}
	for _, s := range added {
		log.Printf("[%s] remote account added (re-read every %s, max_age %s)", s.logName(), s.interval, s.maxAge)
	}
	_, remote := mc.states()
	for _, s := range remote {
		s.fetch()
	}
	return nil
}

// sameCredential reports whether a listed credential is the one a state
// already reads. Status and quota are left out: they move on their own, and
// a new state would start with no cache.
func sameCredential(a, b cliproxy.Credential) bool {
	return a.Name == b.Name && a.AuthIndex == b.AuthIndex
}

// planRemote picks at most one proxy-held credential per (provider, account)
// to poll. It skips disabled credentials, providers with no remote fetcher,
// and accounts already polled through a local login. A credential whose
// status is unavailable is kept: that is the proxy's view of its own traffic,
// and its recorded quota still says something. Among duplicates the one
// already polled stays, else the lowest name wins.
func planRemote(creds []cliproxy.Credential, local map[stateKey]bool, polled map[stateKey]cliproxy.Credential, supported func(provider string) bool) map[stateKey]cliproxy.Credential {
	plan := make(map[stateKey]cliproxy.Credential)
	for _, cred := range creds {
		if cred.Disabled {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(cred.Provider))
		if !supported(id) {
			continue
		}
		key := stateKey{provider: id, account: accountLabel(cred.Email)}
		if local[key] {
			continue
		}

		current, isPolled := polled[key]
		chosen, have := plan[key]
		switch {
		case !have:
			plan[key] = cred
		case isPolled && sameCredential(chosen, current):
			// Keep the credential already being polled.
		case isPolled && sameCredential(cred, current):
			plan[key] = cred
		case cred.Name < chosen.Name:
			plan[key] = cred
		}
	}
	return plan
}

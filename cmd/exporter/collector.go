package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	quotaLabels  = []string{"provider", "account", "category"}
	scrapeLabels = []string{"provider", "account"}
	creditLabels = []string{"provider", "account", "credit"}

	descUtilization = prometheus.NewDesc(
		"llm_quota_utilization_ratio",
		"Current quota utilization (0.0-1.0)",
		quotaLabels, nil,
	)
	descGlide = prometheus.NewDesc(
		"llm_quota_glide_ratio",
		"Expected pacing through quota window (0.0-1.0)",
		quotaLabels, nil,
	)
	descHeadroom = prometheus.NewDesc(
		"llm_quota_headroom_ratio",
		"Fraction of usage ahead of even pacing. Positive: burning too fast, slow down. Negative: under pace, room to use more.",
		quotaLabels, nil,
	)
	descResetTimestamp = prometheus.NewDesc(
		"llm_quota_reset_timestamp_seconds",
		"Unix timestamp when quota resets",
		quotaLabels, nil,
	)
	descWindowSeconds = prometheus.NewDesc(
		"llm_quota_window_seconds",
		"Duration of the quota window in seconds",
		quotaLabels, nil,
	)
	descExtraUtilization = prometheus.NewDesc(
		"llm_extra_utilization_ratio",
		"Monthly extra usage utilization (0.0-1.0)",
		scrapeLabels, nil,
	)
	descExtraGlide = prometheus.NewDesc(
		"llm_extra_glide_ratio",
		"Monthly pacing through calendar month (0.0-1.0)",
		scrapeLabels, nil,
	)
	descExtraUsed = prometheus.NewDesc(
		"llm_extra_used_dollars",
		"Extra usage dollars spent this month",
		scrapeLabels, nil,
	)
	descExtraLimit = prometheus.NewDesc(
		"llm_extra_limit_dollars",
		"Extra usage monthly limit in dollars",
		scrapeLabels, nil,
	)
	descGlideHours = prometheus.NewDesc(
		"llm_quota_glide_hours",
		"Hours into the quota window if usage were perfectly even across the full window",
		quotaLabels, nil,
	)
	descHeadroomHours = prometheus.NewDesc(
		"llm_quota_headroom_hours",
		"Hours of usage ahead of even pacing. Positive: burning too fast, slow down. Negative: under pace, room to use more.",
		quotaLabels, nil,
	)
	descSleepRecovery = prometheus.NewDesc(
		"llm_quota_sleep_recovery_ratio",
		"Fraction of quota window recoverable through remaining sleep periods",
		quotaLabels, nil,
	)
	descAdjustedHeadroom = prometheus.NewDesc(
		"llm_quota_adjusted_headroom_ratio",
		"Headroom after subtracting sleep recovery. Positive: over pace even accounting for sleep.",
		quotaLabels, nil,
	)
	descCreditLimit = prometheus.NewDesc(
		"llm_credit_limit_dollars",
		"Dollar allowance granted by a fixed credit",
		creditLabels, nil,
	)
	descCreditUsed = prometheus.NewDesc(
		"llm_credit_used_dollars",
		"Dollars spent from a fixed credit",
		creditLabels, nil,
	)
	descCreditRemaining = prometheus.NewDesc(
		"llm_credit_remaining_dollars",
		"Dollars left in a fixed credit",
		creditLabels, nil,
	)
	descCreditExpiry = prometheus.NewDesc(
		"llm_credit_expiry_timestamp_seconds",
		"Unix timestamp when a fixed credit expires",
		creditLabels, nil,
	)
	descScrapeSuccess = prometheus.NewDesc(
		"llm_scrape_success",
		"Whether the latest completed provider acquisition attempt succeeded (1=yes, 0=no)",
		scrapeLabels, nil,
	)
	descScrapeFresh = prometheus.NewDesc(
		"llm_scrape_fresh",
		"Whether cached provider data is within its maximum age (1=yes, 0=no)",
		scrapeLabels, nil,
	)
	descScrapeTimestamp = prometheus.NewDesc(
		"llm_scrape_timestamp_seconds",
		"Unix timestamp of last successful provider fetch",
		scrapeLabels, nil,
	)

	allDescs = []*prometheus.Desc{
		descUtilization, descGlide, descHeadroom,
		descGlideHours, descHeadroomHours,
		descSleepRecovery, descAdjustedHeadroom,
		descResetTimestamp, descWindowSeconds,
		descExtraUtilization, descExtraGlide, descExtraUsed, descExtraLimit,
		descCreditLimit, descCreditUsed, descCreditRemaining, descCreditExpiry,
		descScrapeSuccess, descScrapeFresh, descScrapeTimestamp,
	}
)

// unknownAccount labels a state whose account could not be resolved. The
// label is never empty, so a series never loses its place in a dashboard.
const unknownAccount = "unknown"

// accountLabel normalizes an email into the account label.
func accountLabel(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return unknownAccount
	}
	return email
}

// stateKey identifies one poller: one provider for one account.
type stateKey struct {
	provider string
	account  string
}

func (k stateKey) String() string { return k.provider + " " + k.account }

var errUnavailable = errors.New("provider not available")

// nextDelay is how long a poller waits after an attempt: its interval, or
// longer when the server named a Retry-After.
func nextDelay(interval time.Duration, err error) time.Duration {
	if wait, ok := provider.RetryAfter(err); ok && wait > interval {
		return wait
	}
	return interval
}

// providerState polls one provider for one account and holds its last
// successful result.
type providerState struct {
	prov     provider.Provider
	interval time.Duration
	maxAge   time.Duration

	// identity, for a local login, names the account it is using. It is read
	// again before every poll, since the person can log in as someone else.
	identity func() string
	// onIdentityChange runs after identity names a different account.
	onIdentityChange func()

	// remote marks a state read from CLIProxyAPI. It has no polling loop of
	// its own: each credential listing hands it the latest record.
	remote bool

	mu          sync.RWMutex
	account     string
	credential  cliproxy.Credential // remote only, replaced on every listing
	lastResult  *provider.FetchResult
	lastFetch   time.Time // when the cached data was true
	lastSuccess bool

	stop     chan struct{}
	stopOnce sync.Once
}

func newLocalState(p provider.Provider, identity func() string, interval, maxAge time.Duration) *providerState {
	s := &providerState{
		prov:     p,
		interval: interval,
		maxAge:   maxAge,
		identity: identity,
		account:  unknownAccount,
		stop:     make(chan struct{}),
	}
	if identity != nil {
		s.account = accountLabel(identity())
	}
	return s
}

func newRemoteState(p provider.Provider, cred cliproxy.Credential, interval, maxAge time.Duration) *providerState {
	return &providerState{
		prov:       p,
		interval:   interval,
		maxAge:     maxAge,
		remote:     true,
		credential: cred,
		account:    accountLabel(cred.Email),
	}
}

// currentCredential returns the credential from the latest listing.
func (s *providerState) currentCredential() cliproxy.Credential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.credential
}

func (s *providerState) setCredential(cred cliproxy.Credential) {
	s.mu.Lock()
	s.credential = cred
	s.mu.Unlock()
}

// markFailed records a failed attempt without touching the cache.
func (s *providerState) markFailed() {
	s.mu.Lock()
	s.lastSuccess = false
	s.mu.Unlock()
}

func (s *providerState) key() stateKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return stateKey{provider: s.prov.ID, account: accountLabel(s.account)}
}

// logName names the state in log lines. Emails are not secret; tokens and
// keys never reach here.
func (s *providerState) logName() string {
	name := s.key().String()
	if s.remote {
		name += " via cliproxy"
	}
	return name
}

func (s *providerState) run() {
	for {
		select {
		case <-s.stop:
			return
		default:
		}

		err := s.fetch()
		delay := nextDelay(s.interval, err)
		s.logWait(err, delay)

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-s.stop:
			timer.Stop()
			return
		}
	}
}

// logWait records every Retry-After an attempt received, whether or not it
// pushed the next poll out, and every 429 that came without one.
func (s *providerState) logWait(err error, delay time.Duration) {
	wait, hasWait := provider.RetryAfter(err)
	limited := provider.RateLimited(err)
	switch {
	case hasWait && limited:
		log.Printf("[%s] rate limited; server asked to wait %s; next poll in %s", s.logName(), wait, delay)
	case hasWait:
		log.Printf("[%s] server asked to wait %s; next poll in %s", s.logName(), wait, delay)
	case limited:
		log.Printf("[%s] rate limited; no Retry-After; next poll in %s", s.logName(), delay)
	}
}

// halt stops the polling loop. It is safe to call more than once.
func (s *providerState) halt() {
	s.stopOnce.Do(func() {
		if s.stop != nil {
			close(s.stop)
		}
	})
}

// checkIdentity re-reads a local login's account. A different account means
// the cached result describes someone else, so it is dropped.
func (s *providerState) checkIdentity() {
	if s.identity == nil {
		return
	}
	account := accountLabel(s.identity())

	s.mu.Lock()
	previous := s.account
	changed := account != accountLabel(previous)
	if changed {
		s.account = account
		s.lastResult = nil
		s.lastFetch = time.Time{}
		s.lastSuccess = false
	}
	s.mu.Unlock()

	if changed {
		log.Printf("[%s] login changed from %s; cache dropped", s.logName(), accountLabel(previous))
		if s.onIdentityChange != nil {
			s.onIdentityChange()
		}
	}
}

func (s *providerState) fetch() error {
	s.checkIdentity()

	available := s.prov.Probe == nil || s.prov.Probe()
	if !available {
		s.mu.Lock()
		s.lastSuccess = false
		s.mu.Unlock()
		log.Printf("[%s] provider not available, skipping", s.logName())
		return errUnavailable
	}

	result, _, err := s.prov.Fetch()
	if err != nil {
		s.mu.Lock()
		s.lastSuccess = false
		s.mu.Unlock()
		log.Printf("[%s] fetch error: %v", s.logName(), err)
		return err
	}
	if result == nil {
		result = &provider.FetchResult{}
	}

	now := time.Now()
	observed := now
	if !result.ObservedAt.IsZero() {
		observed = result.ObservedAt
	}

	s.mu.Lock()
	s.lastResult = result
	s.lastFetch = observed
	s.lastSuccess = true
	s.mu.Unlock()

	extra := ""
	if result.Extra != nil {
		extra = " + extra usage"
	}
	age := ""
	if !result.ObservedAt.IsZero() {
		age = fmt.Sprintf(" observed %s ago", now.Sub(observed).Round(time.Second))
	}
	log.Printf("[%s] fetched %d categories, %d credits%s%s", s.logName(), len(result.Categories), len(result.Credits), extra, age)
	return nil
}

func (s *providerState) fresh(now time.Time) bool {
	if s.lastResult == nil || s.lastFetch.IsZero() || s.maxAge <= 0 {
		return false
	}
	return now.Sub(s.lastFetch) <= s.maxAge
}

// collect emits this state's series. A state whose key is already in seen
// emits nothing, so two pollers for one account never fail a Gather on
// duplicate series. A nil seen skips that check.
func (s *providerState) collect(ch chan<- prometheus.Metric, seen map[stateKey]bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pid := s.prov.ID
	account := accountLabel(s.account)
	if seen != nil {
		key := stateKey{provider: pid, account: account}
		if seen[key] {
			return
		}
		seen[key] = true
	}
	fresh := s.fresh(time.Now())

	success := 0.0
	if s.lastSuccess {
		success = 1.0
	}
	ch <- prometheus.MustNewConstMetric(descScrapeSuccess, prometheus.GaugeValue, success, pid, account)

	freshness := 0.0
	if fresh {
		freshness = 1.0
	}
	ch <- prometheus.MustNewConstMetric(descScrapeFresh, prometheus.GaugeValue, freshness, pid, account)

	if !s.lastFetch.IsZero() {
		ch <- prometheus.MustNewConstMetric(descScrapeTimestamp, prometheus.GaugeValue, float64(s.lastFetch.Unix()), pid, account)
	}

	if !fresh {
		return
	}

	// Names are labels, and one repeated label value would fail the Gather
	// for every provider, so a repeat is dropped.
	categories := make(map[string]bool, len(s.lastResult.Categories))
	for _, cat := range s.lastResult.Categories {
		if categories[cat.Name] {
			continue
		}
		categories[cat.Name] = true

		usage := cat.Utilization / 100.0
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds) / 100.0
		headroom := usage - glide
		windowHours := float64(cat.WindowSeconds) / 3600.0
		labels := []string{pid, account, cat.Name}

		ch <- prometheus.MustNewConstMetric(descUtilization, prometheus.GaugeValue, usage, labels...)
		ch <- prometheus.MustNewConstMetric(descGlide, prometheus.GaugeValue, glide, labels...)
		ch <- prometheus.MustNewConstMetric(descHeadroom, prometheus.GaugeValue, headroom, labels...)
		ch <- prometheus.MustNewConstMetric(descGlideHours, prometheus.GaugeValue, glide*windowHours, labels...)
		ch <- prometheus.MustNewConstMetric(descHeadroomHours, prometheus.GaugeValue, headroom*windowHours, labels...)
		sleepRecovery := parse.CalcSleepRecoveryPct(cat.ResetsAt, cat.WindowSeconds) / 100.0
		adjustedHeadroom := headroom - sleepRecovery
		ch <- prometheus.MustNewConstMetric(descSleepRecovery, prometheus.GaugeValue, sleepRecovery, labels...)
		ch <- prometheus.MustNewConstMetric(descAdjustedHeadroom, prometheus.GaugeValue, adjustedHeadroom, labels...)
		ch <- prometheus.MustNewConstMetric(descWindowSeconds, prometheus.GaugeValue, float64(cat.WindowSeconds), labels...)

		if cat.ResetsAt != "" {
			t, err := time.Parse(time.RFC3339, cat.ResetsAt)
			if err == nil {
				ch <- prometheus.MustNewConstMetric(descResetTimestamp, prometheus.GaugeValue, float64(t.Unix()), labels...)
			}
		}
	}

	if extra := s.lastResult.Extra; extra != nil {
		ch <- prometheus.MustNewConstMetric(descExtraUtilization, prometheus.GaugeValue, extra.Utilization/100.0, pid, account)
		ch <- prometheus.MustNewConstMetric(descExtraGlide, prometheus.GaugeValue, parse.CalcMonthGlide()/100.0, pid, account)
		ch <- prometheus.MustNewConstMetric(descExtraUsed, prometheus.GaugeValue, extra.UsedCredits/100.0, pid, account)
		ch <- prometheus.MustNewConstMetric(descExtraLimit, prometheus.GaugeValue, extra.MonthlyLimit/100.0, pid, account)
	}

	credits := make(map[string]bool, len(s.lastResult.Credits))
	for _, credit := range s.lastResult.Credits {
		if credits[credit.Name] {
			continue
		}
		credits[credit.Name] = true
		labels := []string{pid, account, credit.Name}

		ch <- prometheus.MustNewConstMetric(descCreditLimit, prometheus.GaugeValue, credit.LimitDollars, labels...)
		ch <- prometheus.MustNewConstMetric(descCreditUsed, prometheus.GaugeValue, credit.UsedDollars, labels...)
		ch <- prometheus.MustNewConstMetric(descCreditRemaining, prometheus.GaugeValue, credit.RemainingDollars, labels...)
		if credit.ExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, credit.ExpiresAt); err == nil {
				ch <- prometheus.MustNewConstMetric(descCreditExpiry, prometheus.GaugeValue, float64(t.Unix()), labels...)
			}
		}
	}
}

// multiCollector is a single prometheus.Collector over every poller: one per
// local login, plus one per proxy-held account when CLIProxyAPI is set up.
type multiCollector struct {
	mu     sync.Mutex
	local  []*providerState
	remote map[stateKey]*providerState

	// Remote polling, unset when no proxy is configured.
	proxy       credentialLister
	factories   map[string]remoteFactory
	relist      time.Duration
	reconcileCh chan struct{}

	// startState begins a state's polling loop. Tests replace it.
	startState func(*providerState)
}

func newMultiCollector() *multiCollector {
	return &multiCollector{
		remote:     make(map[stateKey]*providerState),
		startState: func(s *providerState) { go s.run() },
	}
}

// addLocal registers a poller for a local login. identity names its account
// and may be nil when the provider cannot say.
func (mc *multiCollector) addLocal(p provider.Provider, identity func() string, interval, maxAge time.Duration) *providerState {
	s := newLocalState(p, identity, interval, maxAge)
	s.onIdentityChange = mc.triggerReconcile
	mc.mu.Lock()
	mc.local = append(mc.local, s)
	mc.mu.Unlock()
	return s
}

func (mc *multiCollector) start() {
	mc.mu.Lock()
	local := append([]*providerState(nil), mc.local...)
	mc.mu.Unlock()
	for _, s := range local {
		mc.startState(s)
	}
	if mc.proxy != nil {
		go mc.runReconciler()
	}
}

// states returns the local states in registration order, then the remote
// ones sorted by key, so output order is stable.
func (mc *multiCollector) states() (local, remote []*providerState) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	local = append(local, mc.local...)
	keys := make([]stateKey, 0, len(mc.remote))
	for k := range mc.remote {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].provider != keys[j].provider {
			return keys[i].provider < keys[j].provider
		}
		return keys[i].account < keys[j].account
	})
	for _, k := range keys {
		remote = append(remote, mc.remote[k])
	}
	return local, remote
}

// Describe implements prometheus.Collector.
func (mc *multiCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range allDescs {
		ch <- d
	}
}

// Collect implements prometheus.Collector. Local states go first, so a remote
// state for an account that is also logged in locally is the one skipped.
func (mc *multiCollector) Collect(ch chan<- prometheus.Metric) {
	local, remote := mc.states()
	seen := make(map[stateKey]bool, len(local)+len(remote))
	for _, s := range local {
		s.collect(ch, seen)
	}
	for _, s := range remote {
		s.collect(ch, seen)
	}
}

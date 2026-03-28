package main

import (
	"log"
	"sync"
	"time"

	"github.com/alliprice/headroom/internal/parse"
	"github.com/alliprice/headroom/internal/provider"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	descUtilization = prometheus.NewDesc(
		"llm_quota_utilization_ratio",
		"Current quota utilization (0.0-1.0)",
		[]string{"provider", "category"}, nil,
	)
	descGlide = prometheus.NewDesc(
		"llm_quota_glide_ratio",
		"Expected pacing through quota window (0.0-1.0)",
		[]string{"provider", "category"}, nil,
	)
	descHeadroom = prometheus.NewDesc(
		"llm_quota_headroom_ratio",
		"Fraction of usage ahead of even pacing. Positive: burning too fast, slow down. Negative: under pace, room to use more.",
		[]string{"provider", "category"}, nil,
	)
	descResetTimestamp = prometheus.NewDesc(
		"llm_quota_reset_timestamp_seconds",
		"Unix timestamp when quota resets",
		[]string{"provider", "category"}, nil,
	)
	descWindowSeconds = prometheus.NewDesc(
		"llm_quota_window_seconds",
		"Duration of the quota window in seconds",
		[]string{"provider", "category"}, nil,
	)
	descExtraUtilization = prometheus.NewDesc(
		"llm_extra_utilization_ratio",
		"Monthly extra usage utilization (0.0-1.0)",
		[]string{"provider"}, nil,
	)
	descExtraGlide = prometheus.NewDesc(
		"llm_extra_glide_ratio",
		"Monthly pacing through calendar month (0.0-1.0)",
		[]string{"provider"}, nil,
	)
	descExtraUsed = prometheus.NewDesc(
		"llm_extra_used_dollars",
		"Extra usage dollars spent this month",
		[]string{"provider"}, nil,
	)
	descExtraLimit = prometheus.NewDesc(
		"llm_extra_limit_dollars",
		"Extra usage monthly limit in dollars",
		[]string{"provider"}, nil,
	)
	descGlideHours = prometheus.NewDesc(
		"llm_quota_glide_hours",
		"Hours into the quota window if usage were perfectly even across the full window",
		[]string{"provider", "category"}, nil,
	)
	descHeadroomHours = prometheus.NewDesc(
		"llm_quota_headroom_hours",
		"Hours of usage ahead of even pacing. Positive: burning too fast, slow down. Negative: under pace, room to use more.",
		[]string{"provider", "category"}, nil,
	)
	descScrapeSuccess = prometheus.NewDesc(
		"llm_scrape_success",
		"Whether the last provider fetch succeeded (1=yes, 0=no)",
		[]string{"provider"}, nil,
	)
	descScrapeTimestamp = prometheus.NewDesc(
		"llm_scrape_timestamp_seconds",
		"Unix timestamp of last successful provider fetch",
		[]string{"provider"}, nil,
	)

	allDescs = []*prometheus.Desc{
		descUtilization, descGlide, descHeadroom,
		descGlideHours, descHeadroomHours,
		descResetTimestamp, descWindowSeconds,
		descExtraUtilization, descExtraGlide, descExtraUsed, descExtraLimit,
		descScrapeSuccess, descScrapeTimestamp,
	}
)

// providerState holds the cached fetch result for a single provider.
type providerState struct {
	prov     provider.Provider
	interval time.Duration

	mu          sync.RWMutex
	lastResult  *provider.FetchResult
	lastFetch   time.Time
	lastSuccess bool
}

func (s *providerState) run() {
	s.fetch()
	tick := time.NewTicker(s.interval)
	for range tick.C {
		s.fetch()
	}
}

func (s *providerState) fetch() {
	available := s.prov.Probe == nil || s.prov.Probe()
	if !available {
		log.Printf("[%s] provider not available, skipping", s.prov.ID)
		return
	}

	result, _, err := s.prov.Fetch()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		log.Printf("[%s] fetch error: %v", s.prov.ID, err)
		s.lastSuccess = false
		return
	}

	s.lastResult = result
	s.lastFetch = time.Now()
	s.lastSuccess = true

	n := len(result.Categories)
	extra := ""
	if result.Extra != nil {
		extra = " + extra usage"
	}
	log.Printf("[%s] fetched %d categories%s", s.prov.ID, n, extra)
}

func (s *providerState) collect(ch chan<- prometheus.Metric) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pid := s.prov.ID

	success := 0.0
	if s.lastSuccess {
		success = 1.0
	}
	ch <- prometheus.MustNewConstMetric(descScrapeSuccess, prometheus.GaugeValue, success, pid)

	if !s.lastFetch.IsZero() && s.lastSuccess {
		ch <- prometheus.MustNewConstMetric(descScrapeTimestamp, prometheus.GaugeValue, float64(s.lastFetch.Unix()), pid)
	}

	if s.lastResult == nil {
		return
	}

	for _, cat := range s.lastResult.Categories {
		usage := cat.Utilization / 100.0
		glide := parse.CalcGlideSlope(cat.ResetsAt, cat.WindowSeconds) / 100.0
		headroom := usage - glide
		windowHours := float64(cat.WindowSeconds) / 3600.0

		ch <- prometheus.MustNewConstMetric(descUtilization, prometheus.GaugeValue, usage, pid, cat.Name)
		ch <- prometheus.MustNewConstMetric(descGlide, prometheus.GaugeValue, glide, pid, cat.Name)
		ch <- prometheus.MustNewConstMetric(descHeadroom, prometheus.GaugeValue, headroom, pid, cat.Name)
		ch <- prometheus.MustNewConstMetric(descGlideHours, prometheus.GaugeValue, glide*windowHours, pid, cat.Name)
		ch <- prometheus.MustNewConstMetric(descHeadroomHours, prometheus.GaugeValue, headroom*windowHours, pid, cat.Name)
		ch <- prometheus.MustNewConstMetric(descWindowSeconds, prometheus.GaugeValue, float64(cat.WindowSeconds), pid, cat.Name)

		if cat.ResetsAt != "" {
			t, err := time.Parse(time.RFC3339, cat.ResetsAt)
			if err == nil {
				ch <- prometheus.MustNewConstMetric(descResetTimestamp, prometheus.GaugeValue, float64(t.Unix()), pid, cat.Name)
			}
		}
	}

	if extra := s.lastResult.Extra; extra != nil {
		ch <- prometheus.MustNewConstMetric(descExtraUtilization, prometheus.GaugeValue, extra.Utilization/100.0, pid)
		ch <- prometheus.MustNewConstMetric(descExtraGlide, prometheus.GaugeValue, parse.CalcMonthGlide()/100.0, pid)
		ch <- prometheus.MustNewConstMetric(descExtraUsed, prometheus.GaugeValue, extra.UsedCredits/100.0, pid)
		ch <- prometheus.MustNewConstMetric(descExtraLimit, prometheus.GaugeValue, extra.MonthlyLimit/100.0, pid)
	}
}

// multiCollector is a single prometheus.Collector that wraps all providers.
type multiCollector struct {
	providers []*providerState
}

func newMultiCollector() *multiCollector {
	return &multiCollector{}
}

func (mc *multiCollector) add(p provider.Provider, interval time.Duration) {
	mc.providers = append(mc.providers, &providerState{
		prov:     p,
		interval: interval,
	})
}

func (mc *multiCollector) start() {
	for _, s := range mc.providers {
		go s.run()
	}
}

// Describe implements prometheus.Collector.
func (mc *multiCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range allDescs {
		ch <- d
	}
}

// Collect implements prometheus.Collector.
func (mc *multiCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range mc.providers {
		s.collect(ch)
	}
}

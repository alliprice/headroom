package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/alliprice/headroom/internal/cliproxy"
	"github.com/alliprice/headroom/internal/provider"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.yaml.in/yaml/v2"
)

// revision is the commit this binary was built from, set with
// -ldflags "-X main.revision=<sha>" by scripts/install-exporter.sh.
var revision string

// buildRevision reports revision, else the VCS stamp go build records when it
// runs inside a checkout.
func buildRevision() string {
	if revision != "" {
		return revision
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev string
		var dirty bool
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				rev = setting.Value
			case "vcs.modified":
				dirty = setting.Value == "true"
			}
		}
		if rev != "" {
			if dirty {
				rev += "-dirty"
			}
			return rev
		}
	}
	return "unknown"
}

type config struct {
	Listen    string                    `yaml:"listen"`
	Providers map[string]providerConfig `yaml:"providers"`
	CLIProxy  cliproxyConfig            `yaml:"cliproxy"`
}

// cliproxyConfig points the exporter at a CLIProxyAPI server, whose
// credentials are polled as extra accounts. An empty URL turns that off.
type cliproxyConfig struct {
	URL        string `yaml:"url"`
	KeyService string `yaml:"key_service"`
	Relist     string `yaml:"relist"`
	MaxAge     string `yaml:"max_age"`
}

func (cc cliproxyConfig) validate() error {
	if _, err := positiveDuration("relist", cc.Relist); err != nil {
		return err
	}
	if _, err := positiveDuration("max_age", cc.MaxAge); err != nil {
		return err
	}
	if cc.URL == "" {
		return nil
	}
	u, err := url.Parse(cc.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("bad url %q: want http://host:port", cc.URL)
	}
	if strings.TrimSpace(cc.KeyService) == "" {
		return fmt.Errorf("key_service must not be empty")
	}
	return nil
}

func (cc cliproxyConfig) relistInterval() time.Duration {
	d, _ := positiveDuration("relist", cc.Relist)
	return d
}

// maxAge is how old a remote account's recorded quota may be and still be
// shown. The record is only as new as the last request the proxy sent with
// that credential.
func (cc cliproxyConfig) maxAge() time.Duration {
	d, _ := positiveDuration("max_age", cc.MaxAge)
	return d
}

// fallbackInterval is the poll interval for a provider the config does not
// mention.
const fallbackInterval = "20m"

// providerConfig returns the settings for provider id, falling back to the
// default interval and twice that as max_age.
func (c config) providerConfig(id string) providerConfig {
	if pc, ok := c.Providers[id]; ok {
		return pc
	}
	return providerConfig{Interval: fallbackInterval}
}

// unknownProviders lists provider keys in the config that name no provider,
// sorted. They are kept, since a typo should not stop the exporter, but are
// worth a warning.
func (c config) unknownProviders() []string {
	var unknown []string
	for id := range c.Providers {
		if provider.ByID(id) == nil {
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	return unknown
}

type providerConfig struct {
	Interval string `yaml:"interval"`
	MaxAge   string `yaml:"max_age"`
}

func (pc providerConfig) durations() (interval, maxAge time.Duration, err error) {
	interval, err = positiveDuration("interval", pc.Interval)
	if err != nil {
		return 0, 0, err
	}
	if pc.MaxAge != "" {
		maxAge, err = positiveDuration("max_age", pc.MaxAge)
		return interval, maxAge, err
	}

	maxAge = 2 * interval
	if maxAge <= 0 {
		return 0, 0, fmt.Errorf("twice interval %q overflows max_age", pc.Interval)
	}
	return interval, maxAge, nil
}

func positiveDuration(field, value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("bad %s %q: %w", field, value, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("bad %s %q: must be positive", field, value)
	}
	return duration, nil
}

func defaultConfig() config {
	return config{
		Listen: ":9101",
		Providers: map[string]providerConfig{
			// 75m covers a success, then a 429 whose Retry-After (about 2593s
			// seen live) pushes the next poll to about +63m.
			"claude":      {Interval: "20m", MaxAge: "75m"},
			"antigravity": {Interval: "20m"},
			"codex":       {Interval: "10m", MaxAge: "35m"},
		},
		CLIProxy: cliproxyConfig{
			KeyService: cliproxy.DefaultKeyService,
			Relist:     "5m",
			MaxAge:     "6h",
		},
	}
}

func loadConfig(path string) (config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	// YAML map values replace structs rather than merging their fields. Decode
	// optional overrides separately so partial entries retain provider defaults.
	var overrides struct {
		Listen    *string `yaml:"listen"`
		Providers map[string]struct {
			Interval *string `yaml:"interval"`
			MaxAge   *string `yaml:"max_age"`
		} `yaml:"providers"`
		CLIProxy *struct {
			URL        *string `yaml:"url"`
			KeyService *string `yaml:"key_service"`
			Relist     *string `yaml:"relist"`
			MaxAge     *string `yaml:"max_age"`
		} `yaml:"cliproxy"`
	}
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if overrides.Listen != nil {
		cfg.Listen = *overrides.Listen
	}
	if cp := overrides.CLIProxy; cp != nil {
		if cp.URL != nil {
			cfg.CLIProxy.URL = strings.TrimSpace(*cp.URL)
		}
		if cp.KeyService != nil {
			cfg.CLIProxy.KeyService = *cp.KeyService
		}
		if cp.Relist != nil {
			cfg.CLIProxy.Relist = *cp.Relist
		}
		if cp.MaxAge != nil {
			cfg.CLIProxy.MaxAge = *cp.MaxAge
		}
		if err := cfg.CLIProxy.validate(); err != nil {
			return cfg, fmt.Errorf("cliproxy: %w", err)
		}
	}
	for id, override := range overrides.Providers {
		pc, ok := cfg.Providers[id]
		if !ok {
			pc.Interval = fallbackInterval
		}
		if override.Interval != nil {
			pc.Interval = *override.Interval
		}
		if override.MaxAge != nil {
			pc.MaxAge = *override.MaxAge
			if pc.MaxAge == "" {
				return cfg, fmt.Errorf("provider %s: bad max_age %q: must be positive", id, pc.MaxAge)
			}
		}
		if _, _, err := pc.durations(); err != nil {
			return cfg, fmt.Errorf("provider %s: %w", id, err)
		}
		cfg.Providers[id] = pc
	}
	return cfg, nil
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "headroom-exporter", "config.yaml")
}

// localIdentity names the account a local login is using, or nil when the
// provider cannot say.
func localIdentity(id string) func() string {
	switch id {
	case "claude":
		return provider.ClaudeLoginEmail
	case "codex":
		return provider.CodexLoginEmail
	default:
		return nil
	}
}

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config file")
	showVersion := flag.Bool("version", false, "print the build revision and exit")
	flag.Parse()

	rev := buildRevision()
	if *showVersion {
		fmt.Println(rev)
		return
	}
	log.Printf("headroom-exporter revision %s", rev)

	cfg, err := loadConfig(*configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("no config at %s, using defaults", *configPath)
			cfg = defaultConfig()
		} else {
			log.Fatalf("loading config: %v", err)
		}
	} else {
		log.Printf("loaded config from %s", *configPath)
	}
	for _, id := range cfg.unknownProviders() {
		log.Printf("warning: config names unknown provider %q; it has no poller", id)
	}

	mc := newMultiCollector()
	for _, p := range provider.All {
		interval, maxAge, err := cfg.providerConfig(p.ID).durations()
		if err != nil {
			log.Fatalf("provider %s: %v", p.ID, err)
		}
		s := mc.addLocal(p, localIdentity(p.ID), interval, maxAge)
		log.Printf("registered %s (interval %s, max_age %s)", s.logName(), interval, maxAge)
	}

	if cfg.CLIProxy.URL != "" {
		client := &cliproxy.Client{
			BaseURL: cfg.CLIProxy.URL,
			Key:     cliproxy.KeychainKey(cfg.CLIProxy.KeyService),
			HTTP:    &http.Client{Timeout: reconcileTimeout},
		}
		mc.enableRemote(client, remoteFactories(cfg), cfg.CLIProxy.relistInterval())
		log.Printf("cliproxy at %s (relist %s, max_age %s)", cfg.CLIProxy.URL, cfg.CLIProxy.relistInterval(), cfg.CLIProxy.maxAge())
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(mc)
	mc.start()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<html><body><a href="/metrics">Metrics</a></body></html>`)
	})

	log.Printf("listening on %s", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, nil))
}

package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alliprice/headroom/internal/provider"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.yaml.in/yaml/v2"
)

type config struct {
	Listen    string                    `yaml:"listen"`
	Providers map[string]providerConfig `yaml:"providers"`
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
			"claude":      {Interval: "20m"},
			"antigravity": {Interval: "20m"},
			"codex":       {Interval: "10m", MaxAge: "35m"},
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
	}
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if overrides.Listen != nil {
		cfg.Listen = *overrides.Listen
	}
	for id, override := range overrides.Providers {
		pc, ok := cfg.Providers[id]
		if !ok {
			pc.Interval = "20m"
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

func main() {
	configPath := flag.String("config", defaultConfigPath(), "path to config file")
	flag.Parse()

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

	mc := newMultiCollector()
	for _, p := range provider.All {
		pc, ok := cfg.Providers[p.ID]
		if !ok {
			pc.Interval = "20m"
		}
		interval, maxAge, err := pc.durations()
		if err != nil {
			log.Fatalf("provider %s: %v", p.ID, err)
		}
		mc.add(p, interval, maxAge)
		log.Printf("registered %s (interval %s, max_age %s)", p.ID, interval, maxAge)
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

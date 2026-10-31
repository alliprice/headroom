# ｈｅａｄｒｏｏｍ

> *you are not rate limited. you simply have not yet perceived the space between the bars.*

![demo](demo.gif)

a terminal dashboard for the invisible walls between you and the machines that write your code. it floats above your workflow like a neon sign above a rain-slicked boulevard at 3am. you glance up. the bars tell you everything. you keep typing.

## the vibe

headroom renders your API usage as living progress bars over a procedural plasma background. it breathes. it pulses. it knows when you're looking and when you've walked away.

- **session & weekly limits** for whatever's installed. it checks the usual hiding spots.
- **extra usage tracking** - your monthly billing burn rate, visualized
- **glide slopes** - faint markers showing where usage *should* be if you're pacing evenly through the window. are you ahead of the curve or behind it? the bar knows. the bar always knows.
- **drag to reorder** - grab any bar or panel and rearrange. hold a bar over the trash icon and let go. it's gone. press `0` to bring everything back from the void.
- **auto-sleep** - leave the terminal unfocused and headroom fades into a plasma screensaver. touch any key and it wakes, stretching, bars sweeping up from zero like a city powering on at dusk.

## install

```
go install github.com/alliprice/headroom@latest
```

or clone and build:

```
git clone https://github.com/alliprice/headroom.git
cd headroom
go build -o headroom .
```

## run

```
./headroom
```

that's it. headroom finds your credentials where they live - session cookies, oauth tokens, app servers. if the tool left a trail, headroom follows it. no config files. no env vars. no yaml. just vibes.

### flags

| flag | what it does |
|------|-------------|
| `--demo` | auto-play demo with mock data. no auth needed. just watch. |
| `--debug-sleep` | start in sleep mode (plasma screensaver). any key wakes. |

## keys

| key | effect |
|-----|--------|
| `r` | refresh now |
| `t` | change refresh interval (seconds) |
| `0` | restore all hidden bars |
| `q` / `ctrl+c` | quit |

## prometheus exporter

`cmd/exporter` serves the same numbers on `/metrics` for Grafana. there is one poller per provider account. a poller that gets a `Retry-After` waits it out and logs the wait.

```
scripts/install-exporter.sh [revision]   # build from git archive, swap into ~/.local/bin, restart launchd
headroom-exporter -version               # the commit it was built from
```

config lives at `~/.config/headroom-exporter/config.yaml`. every key is optional:

```yaml
listen: ":9101"
providers:              # interval / max_age; max_age defaults to 2x interval
  claude:      {interval: 20m, max_age: 75m}
  codex:       {interval: 10m, max_age: 35m}
  antigravity: {interval: 20m}
cliproxy:               # read accounts held by CLIProxyAPI; empty url turns it off
  url: http://127.0.0.1:8317
  key_service: cliproxyapi-management   # keychain item with the management key
  relist: 5m                            # how often to re-read its credentials
  max_age: 6h                           # oldest recorded quota still shown
```

local logins are polled directly, with the account read from `~/.claude.json` and `~/.codex/auth.json`.

CLIProxyAPI accounts are never polled upstream. the proxy records the `anthropic-ratelimit-unified-*` headers of the last response it got for each credential, and the exporter reads that record from its credential listing. so a remote account has Session and Weekly only (no per-model limits, no credits), and the numbers change only when traffic flows through the proxy. the data's age is the record's `observed_at`, which is what `llm_scrape_timestamp_seconds` shows and what `cliproxy.max_age` counts from. a window whose reset has passed since then reads 0. only claude credentials are read; disabled ones and accounts that are also logged in locally are skipped.

| metric | labels |
|--------|--------|
| `llm_quota_{utilization,glide,headroom,sleep_recovery,adjusted_headroom}_ratio`, `llm_quota_{glide,headroom}_hours`, `llm_quota_reset_timestamp_seconds`, `llm_quota_window_seconds` | provider, account, category |
| `llm_extra_{utilization,glide}_ratio`, `llm_extra_{used,limit}_dollars` | provider, account |
| `llm_credit_{limit,used,remaining}_dollars`, `llm_credit_expiry_timestamp_seconds` | provider, account, credit |
| `llm_scrape_success`, `llm_scrape_fresh`, `llm_scrape_timestamp_seconds` | provider, account |

`account` is the lowercased login email, or `unknown`. quota, extra and credit series are left out once a poller's data is older than its `max_age`. `llm_scrape_success` is 0 after a failed attempt, or for a remote account the proxy has no record for yet.

## requirements

- go 1.24+
- a terminal that supports 256 colors and mouse events
- at least one subscription to something that writes code and counts the asks
- the low hum of a mass-produced future

## architecture

```
internal/
  auth/     - credential extraction from claude code's session store
  fetch/    - HTTP calls to provider usage APIs. each one a different dialect.
  parse/    - response parsing, glide slope math, time formatting
  provider/ - provider registry. declarative structs, not interfaces.
  tui/      - the whole show. bubbletea model, plasma renderer,
              drag system, layout engine, animation choreography
```

this is what happens when you get nerd-sniped by a rate limit page. wildly overbuilt and fully functional. it has a choreographed loading animation, a procedural plasma screensaver, a full drag-and-drop system with ghost cursors and a pixel-art trash can, and a layout engine that does progressive compaction. it displays four numbers.

built on [bubble tea v2](https://github.com/charmbracelet/bubbletea) and [lip gloss v2](https://github.com/charmbracelet/lipgloss). the plasma background is a real-time sinusoidal interference pattern mapped to a carefully curated gradient that absolutely nobody asked for.

## philosophy

every dashboard is a window into a system that does not care about you. headroom does not pretend otherwise. it simply shows you the numbers, beautifully, so you can make your choices with open eyes.

the progress bars are not metaphors. they are measurements. but if you stare at them long enough at 2am while the plasma shifts behind them, you might feel something. that's between you and the terminal.

---

*ｍａｄｅ ｗｉｔｈ ｉｎｓｏｍｎｉａ ａｎｄ ｇｒａｄｉｅｎｔ ｍａｐｓ*

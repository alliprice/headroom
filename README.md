# ｈｅａｄｒｏｏｍ

> *you are not rate limited. you simply have not yet perceived the space between the bars.*

![demo](demo.gif)

a terminal dashboard for your claude & codex rate limits. it floats above your workflow like a neon sign above a rain-slicked boulevard at 3am. you glance up. the bars tell you everything. you keep typing.

## the vibe

headroom renders your API usage as living progress bars over a procedural plasma background. it breathes. it pulses. it knows when you're looking and when you've walked away.

- **session & weekly limits** for claude and codex, side by side
- **extra usage tracking** — your monthly billing burn rate, visualized
- **glide slopes** — faint markers showing where usage *should* be if you're pacing evenly through the window. are you ahead of the curve or behind it? the bar knows. the bar always knows.
- **drag to reorder** — grab any bar or panel and rearrange. hold a bar over the trash icon and let go. it's gone. press `0` to bring everything back from the void.
- **auto-sleep** — leave the terminal unfocused and headroom fades into a plasma screensaver. touch any key and it wakes, stretching, bars sweeping up from zero like a city powering on at dusk.

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

that's it. headroom reads your claude session cookie from `~/.claude/credentials.json` (the same one claude code uses). no config files. no env vars. no yaml. just vibes.

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

## requirements

- go 1.24+
- a terminal that supports 256 colors and mouse events
- an active claude pro/team/enterprise subscription
- the low hum of a mass-produced future

## architecture

```
internal/
  auth/     — credential extraction from claude code's session store
  fetch/    — HTTP calls to the claude and codex usage APIs
  parse/    — response parsing, glide slope math, time formatting
  tui/      — the whole show. bubbletea model, plasma renderer,
              drag system, layout engine, animation choreography
```

this is what happens when you get nerd-sniped by a rate limit page. wildly overbuilt and fully functional. it has a choreographed loading animation, a procedural plasma screensaver, a full drag-and-drop system with ghost cursors and a pixel-art trash can, and a layout engine that does progressive compaction. it displays four numbers.

built on [bubble tea v2](https://github.com/charmbracelet/bubbletea) and [lip gloss v2](https://github.com/charmbracelet/lipgloss). the plasma background is a real-time sinusoidal interference pattern mapped to a carefully curated gradient that absolutely nobody asked for.

## philosophy

every dashboard is a window into a system that does not care about you. headroom does not pretend otherwise. it simply shows you the numbers, beautifully, so you can make your choices with open eyes.

the progress bars are not metaphors. they are measurements. but if you stare at them long enough at 2am while the plasma shifts behind them, you might feel something. that's between you and the terminal.

---

*ｍａｄｅ ｗｉｔｈ ｉｎｓｏｍｎｉａ ａｎｄ ｇｒａｄｉｅｎｔ ｍａｐｓ*

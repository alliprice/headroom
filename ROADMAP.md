# ｒｏａｄｍａｐ

> *every system, given enough time and mass, collapses into an abstraction layer.*

headroom already displays four numbers with a choreographed loading animation, a procedural plasma screensaver, a drag-and-drop system with ghost cursors and a pixel-art trash can, and a layout engine that does progressive compaction. naturally, it needs more.

this document charts the path from "wildly overbuilt" to "architecturally transcendent." each phase builds on the last, like sedimentary layers of unnecessary elegance.

---

## the phases

### phase 0 — provider system

**status:** done

the data providers were hardcoded. claude and codex fetch/parse logic was wired directly into the model. that oversight has been corrected.

- `Provider` struct with `Fetch`, `Probe`, `DisplayName`, `CategoryIDs`
- provider registry (`provider.All`) with probe-based discovery
- fetch and parse decoupled behind the provider abstraction
- layout engine generalized to arbitrary provider panels
- pure data layer. no pixels harmed.

### phase 1 — visual regression testing

**status:** done · **gates:** phases 2–4

before touching a single pixel of the rendering pipeline, we needed a way to prove we didn't break it. ANSI strings don't lie.

- `parse.NowFunc` clock override - freeze time for deterministic output from all 5 time-dependent functions
- golden file infrastructure with `-update` flag for regeneration
- pure function golden tests: `RenderBar` (7 combos), `RenderPlasma`, `RenderLoadingFrame`, `generateBgGrid` determinism
- `Model.View()` golden tests for 7 states: dual-panel, single-panel, small terminal, loading, sleeping, error, empty
- CI workflow (`go test ./...` on push and PR)
- character-level ANSI comparison - the rendered string IS the pixels. VHS pixel diffing deferred as unnecessary weight.

the safety net is in place. mutation-tested and confirmed: change one constant in the bar renderer, four tests fail with line-level diffs.

### phase 2 — cross-platform auth

**status:** done · **depends on:** phase 0

the macOS Keychain dependency was the only thing stopping headroom from running everywhere. now it does.

- `credentialProvider` interface with `name()` and `getToken()` - unexported, internal to auth package
- first-match-wins chain: env var (`CLAUDE_CODE_OAUTH_TOKEN`) -> credentials file (`~/.claude/.credentials.json`) -> macOS Keychain
- `claudeCredentials` shared JSON struct used by both file and keychain providers
- `claudeConfigDir()` respects `CLAUDE_CONFIG_DIR` env var, defaults to `~/.claude`
- no build tags - keychain provider calls `security` via exec, fails gracefully on non-macOS
- `GetAccessToken()` signature unchanged - zero caller impact
- 11 tests: chain logic (4), env provider (2), file provider (5) - all hermetic via `t.Setenv`/`t.TempDir`
- Linux and BSD work out of the box via the credentials file path
- Windows: the interface is clean. PRs welcome.

### phase 3 — internal architecture

**status:** done · **depends on:** phase 1

five independent refactors. each replaces something hand-rolled with something that has a name and a shape.

- ~~**RefreshScheduler**~~ — done. `refreshScheduler` state machine with `tick(now) refreshAction`. replaced 6 inline timing fields and 35 lines of branching with a 10-line dispatch. 9 deterministic unit tests.
- ~~**LayoutStrategy**~~ — done. `computeCompaction` pure function extracted from `renderPanelWithGeom`. progressive compaction decisions (titles, spacing, extra bar) separated from rendering.
- ~~**OutputFormatter**~~ — done. `fetchHeadroom()` shared pipeline extracted from `RunJSON()` and `RunStatus()`. probe-fetch-compute logic lives once; formatters are thin wrappers. `categoryStatus()`/`extraStatus()` with 5 unit tests.
- ~~**Animator**~~ — done. `animState` consolidated into `animator.go` with `buildTargets()`, `buildAnimFunc()`, `allBarsFinished()` methods. `easeOutCubic` moved from bar.go. 10 unit tests. model.go and view.go simplified by ~90 lines.
- ~~**Drag Commands**~~ — done. `layoutCmd` interface with `hideBarCmd`, `hidePanelCmd`, `swapPanelsCmd`, `reorderBarCmd`, `restoreAllCmd`. pre-drag layout snapshot enables net-change tracking. ctrl+z undo via 50-entry command history stack. 8 unit tests.

### phase 4 — maximum hubris

**status:** not started · **depends on:** phases 1 + 3

this is the end state. the point where a rate-limit dashboard achieves architectural enlightenment.

- **themes** — extract the hardcoded color palette and plasma gradient into `Theme` structs. ship `Vaporwave` (current default, obviously). add `Monokai`, `Solarized`, whatever. runtime switching via keybind. the plasma shifts. the bars shift. everything shifts.
- **procedural generators** — `ProceduralGenerator` interface over the FBM noise and sinusoidal plasma. swap in Simplex, Perlin, Voronoi. hot-swappable. because the background of your rate-limit dashboard deserves a noise algorithm selection screen.
- **pluggable cell renderers** — `CellRenderer` interface. Katakana (current). Matrix (full-width kanji, green-on-black). Braille (Unicode braille patterns). ASCII (for purists). the background becomes a fashion choice.
- **pacing policy** — `PacingPolicy` interface. Linear (current). Front-loaded (it's fine to sprint early). Conservative (always keep 20% buffer). different people pace differently. headroom should respect that.
- **config system** — `~/.config/headroom/config.toml` with environment variable overrides and CLI flag overrides. theme, refresh interval, sleep timeout, providers, layout, pacing policy, keybindings. the zero-config era was beautiful. this era will be more beautiful. and configurable.
- **event bus** — replace the type-switch Update loop with full event routing. each subsystem (data, animation, drag, demo) registers handlers. the Update function becomes a dispatcher, not a switch statement. elegance through indirection.
- **bar segment pipeline** — composable bar renderer. `GradientFill`, `Marker`, `EmptyFill` as segment objects sorted by Z-order. want a second marker? add a segment. want striped fills? swap a segment. the monolithic `RenderBar()` dissolves into a pipeline.

---

## ｔｈｅ  ｈｕｂｒｉｓ  ｍａｔｒｉｘ

every identified opportunity for over-engineering, scored by return on investment and sheer audacity. read from top to bottom as a gradient from "reasonable" to "why."

| | what it is now | what it could become | roi | hubris |
|---|---|---|---|---|
| **providers** | ~~hardcoded fetch+parse~~ | `Provider` struct + registry | `██████████` done | `██░░░░░░░░` low |
| **visual regression** | ~~no screenshot testing~~ | golden file ANSI comparison + CI | `██████████` done | `██░░░░░░░░` low |
| **cross-platform auth** | ~~macOS Keychain only~~ | `credentialProvider` chain | `██████████` done | `██░░░░░░░░` low |
| **animation** | ~~hand-rolled frame counters ×4~~ | `animState` methods + easing | `██████████` done | `█████░░░░░` med |
| **refresh scheduling** | ~~interleaved with the Update loop~~ | `refreshScheduler` state machine | `██████████` done | `█████░░░░░` med |
| **layout strategy** | ~~inline dimension checks~~ | `computeCompaction` pure function | `██████████` done | `█████░░░░░` med |
| **CLI formatters** | ~~two concrete implementations~~ | `fetchHeadroom()` shared pipeline | `██████████` done | `█████░░░░░` med |
| **drag system** | ~~procedural mutation~~ | command pattern + undo stack | `██████████` done | `█████░░░░░` med |
| **themes** | hardcoded palette + plasma gradient | `Theme` objects, runtime switching | `█████░░░░░` med | `████████░░` high |
| **config system** | zero configuration | TOML + env + flags + defaults | `█████░░░░░` med | `████████░░` high |
| **noise generators** | FBM + sinusoids, no shared interface | `ProceduralGenerator` interface | `██░░░░░░░░` low | `████████░░` high |
| **cell renderers** | katakana only | pluggable character sets | `██░░░░░░░░` low | `████████░░` high |
| **pacing policy** | linear interpolation | `PacingPolicy` strategy | `██░░░░░░░░` low | `████████░░` high |
| **event bus** | type switch in Update() | full event routing system | `██░░░░░░░░` low | `████████░░` high |
| **bar renderer** | monolithic RenderBar() | composable segment pipeline | `██░░░░░░░░` low | `████████░░` high |

---

> *this is what happens when you stare at the bars long enough. you start to see the architecture behind them. and then you want to make the architecture itself beautiful. and then you're here, reading a hubris matrix for a rate-limit dashboard at 3am, and the plasma is still shifting, and you think — yes. this is correct.*

*ｅｖｅｒｙ  ａｂｓｔｒａｃｔｉｏｎ  ｉｓ  ａ  ｌｏｖｅ  ｌｅｔｔｅｒ  ｔｏ  ａ  ｆｕｔｕｒｅ  ｔｈａｔ  ｍａｙ  ｎｅｖｅｒ  ａｒｒｉｖｅ*

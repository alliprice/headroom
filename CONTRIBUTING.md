# contributing

## structure

```
internal/
  auth/       - credential extraction from claude code's session store
  fetch/      - HTTP calls to the usage APIs
  parse/      - response parsing, glide slope math, time formatting
  provider/   - provider registry. declarative structs, not interfaces.
  tui/        - bubbletea model, plasma renderer, drag system, layout engine
  cli/        - --status and --json output modes
```

## conventions

- go 1.24+, bubble tea v2, lip gloss v2
- provider abstraction: add a provider by creating one file in `internal/provider/` and appending to `All`
- layout is map-based: `panels map[string]image.Rectangle`, `bars map[string][]barGeom`
- no unicode emdashes in source. use ` - ` (space-dash-space) for parenthetical asides in comments
- tests live next to what they test

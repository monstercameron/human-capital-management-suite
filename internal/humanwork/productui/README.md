# Production UI composition

This package is the GoWebComponents composition layer for the production web
application. It accepts already-authorized presentation models and emits
deterministic server-rendered pages. It owns no workflow, authorization,
credential, persistence, or integration truth.

## Dependency direction

```text
HTTP / future Go-WASM adapter
           |
           v
      ViewProvider  <--- gRPC projection adapter or fixture provider
           |
           v
       PageRegistry
           |
           v
  AppShell + Feature Page + Shared Components
           |
           v
    GoWebComponents renderer
```

The page registry is the only canonical inventory of page identity, route,
title, navigation eligibility, ordering, and renderer. HTTP adapters resolve
routes through the registry instead of casting URL segments into page IDs.

## Source ownership

Ownership is by file family, and this list is not exhaustive. `registry.go`
is the canonical inventory of page identity, routes, titles, navigation
eligibility, ordering, and renderers.

- Pages: over 130 `page_*.go` surfaces, one feature surface and its private
  subcomponents per file.
- Shell and frame: `shell.go` (header, navigation, page frame, landmarks),
  `components.go` (reusable product primitives), `render.go` (document
  assembly only).
- Navigation: `navigation_components.go`, `history_navigation.go`.
- Tables and data display: `data_table.go`, `selectors.go` (pure filtering
  and selection over authorized models), `model.go` (presentation contracts
  and development fixtures).
- Typed style migrations: `typed_mig_A.go`–`typed_mig_F.go`, `styles.go`
  (semantic platform and component styles), `theme.go` (validated semantic
  token compiler and protected accessibility boundaries), `appearance.go`
  (closed customer palette, shape, density, glyph, and motion presets),
  `appearance_components.go` (storage-independent customer appearance
  editor).
- Launching and widgets: `action_launcher.go`, `widget.go`.
- Transport boundary: `provider.go` (transport-neutral projection boundary).

## Adding a page

1. Add its stable `PageID` and one `PageDefinition`.
2. Implement its renderer in a dedicated `page_<feature>.go` file.
3. Accept only an already-authorized `View`; add typed view fields when needed.
4. Compose shared primitives instead of duplicating shell or surface markup.
5. Add route, deterministic-render, accessibility, and authorization tests.
6. Keep mutations behind a registered semantic action; pages never invent
   business authority.

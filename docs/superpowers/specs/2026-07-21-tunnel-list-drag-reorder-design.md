# Drag-to-reorder the tunnel list

Date: 2026-07-21
Status: approved, ready for planning

## Problem

The tunnel list is hard-sorted alphabetically by name, inline at render time:

```tsx
// web/src/components/TunnelList.tsx:60-61
{[...tunnels]
  .sort((a, b) => a.name.localeCompare(b.name))
```

There is no way to arrange tunnels by personal priority — the ones you actually use daily sink below the ones you named "a-something". Users want to drag rows into an order that means something to them.

## Solution

Let the user drag rows by a dedicated grip handle. The resulting order is stored in the browser as a list of tunnel IDs and applied at render, replacing the alphabetical sort.

### Decisions

| Decision | Choice | Why |
|---|---|---|
| Persistence | `localStorage` only | No backend changes at all. `internal/` and `pkg/` are untouched. |
| Drag affordance | Dedicated grip handle | The row contains three buttons and a link; a handle avoids disambiguating drag from click entirely. |
| New/unknown tunnels | Land at the top | A tunnel created via CLI or another browser should be noticed, not buried. |
| Settling | Claim their spot on first sight | Written into the saved order immediately, so the list is stable between arrivals. |
| Library | `@dnd-kit` | Keyboard + touch + auto-scroll for free; idiomatic with the Radix/Tailwind stack. |
| Tests | Vitest, pure function only | The ordering logic is the risky part and needs no DOM. |

### Non-goals

- No server-side ordering. No new column, no new endpoint, no Go changes.
- No sort/filter UI. Manual order *replaces* alphabetical rather than coexisting with a toggle.
- No reordering in the `Metrics`, `Topology`, or `Monitoring` views — those render tunnels too, but are out of scope.
- No multi-select drag, no drag between lists.

## Constraints discovered in the codebase

These three facts shape the design and are why the order cannot simply live in the tunnel objects.

**1. The list is fully replaced every 5 seconds.** `useTunnels()` polls with `refetchInterval: 5000` and calls `setTunnels(query.data)` — a whole-array replacement (`web/src/lib/queries.ts:24,30-32`). Any order written into the array itself is gone within 5s. Order must be a *separate* ID list applied at render.

**2. The server returns tunnels in random order on every request.** `Manager.List()` iterates a `map[string]*Tunnel` (`internal/tunnel/manager.go:467-477`), and Go randomizes map iteration. The `ORDER BY created_at DESC` in SQLite (`internal/storage/sqlite.go:290`) only affects boot-time load into the map, not the API response. The alphabetical sort at `TunnelList.tsx:60` is the only thing hiding this today. **Consequence: every fallback path must be deterministic**, or the random order leaks through and rows shuffle every 5 seconds.

**3. `setTunnels` runs during render**, not in an effect (`web/src/lib/queries.ts:30-32`). This is a pre-existing React anti-pattern. We do not fix it here, but it means our reconciliation must go in a `useEffect` — writing to a zustand store mid-render would compound the violation.

## Design

### Components

Three new units, each independently understandable and testable:

```
web/src/lib/tunnelOrder.ts        pure functions — no React, no storage
web/src/store/orderStore.ts       persisted zustand store — just holds string[]
web/src/components/TunnelList.tsx wiring — DndContext, handle, effect
```

### `tunnelOrder.ts` — the pure core

Two functions, no dependencies beyond the `Tunnel` type.

```ts
orderTunnels(tunnels: Tunnel[], saved: string[]): Tunnel[]
reconcileOrder(tunnels: Tunnel[], saved: string[]): string[] | null
```

**`orderTunnels`** decides what the user sees, on every render:

1. Build an id→tunnel map.
2. `known` = `saved` mapped through it, dropping ids with no matching tunnel (stale entries from deleted tunnels).
3. `unknown` = tunnels whose id is not in `saved`, **sorted by name**.
4. Return `[...unknown, ...known]`.

**`reconcileOrder`** decides what gets persisted, in an effect:

1. `kept` = `saved` filtered to ids that still exist (prunes deleted tunnels).
2. `unknownIds` = ids of tunnels not in `saved`, ordered by their tunnels' names.
3. `next` = `[...unknownIds, ...kept]`.
4. **Return `null` if `next` is element-wise equal to `saved`.** This is load-bearing — without it the effect writes on every poll, re-renders, and loops.

Note that first-run seeding needs no special case. With `saved === []`, every tunnel is "unknown", so step 2 sorts them all by name and the result is exactly the alphabetical order users see today. Nothing jumps on upgrade.

The two functions agree by construction: after `reconcileOrder` runs, `unknown` is empty and `orderTunnels` returns `known` in saved sequence. Render is correct *immediately*, before the effect fires — the effect only persists.

### `orderStore.ts` — persistence

A persisted zustand store, matching the existing `themeStore.ts` and `settingsStore.ts` conventions:

```ts
interface OrderStore {
  order: string[]
  setOrder: (ids: string[]) => void
}
// persist key: 'lazytunnel-tunnel-order'
```

Deliberately dumb — it holds an array. All logic lives in `tunnelOrder.ts` where it can be tested without mocking storage.

### `TunnelList.tsx` — wiring

**Ordering.** Replace lines 60-61. `const ordered = orderTunnels(tunnels, order)`, then map over `ordered`.

**Reconciliation effect:**

```ts
useEffect(() => {
  if (isDemoMode) return          // see Demo mode below
  const next = reconcileOrder(tunnels, order)
  if (next) setOrder(next)
}, [tunnels, order, isDemoMode])
```

**Drag context.** Wrap the `<ul>` in `DndContext` + `SortableContext`:

- Sensors: `PointerSensor` with `activationConstraint: { distance: 5 }` so a stray click never starts a drag, plus `KeyboardSensor` with `sortableKeyboardCoordinates`.
- Collision: `closestCenter`. Strategy: `verticalListSortingStrategy`.
- Modifiers: `restrictToVerticalAxis`, `restrictToParentElement`.
- `items` = the ids of `ordered` (not of `saved`) — so a drag works correctly even in the window before the effect has persisted a newly-arrived tunnel.

**On drag end:** if `over` exists and `active.id !== over.id`, compute `arrayMove(orderedIds, oldIndex, newIndex)` and `setOrder` the whole resulting array. Persisting the full displayed order (rather than splicing into `saved`) keeps the two in sync in one step.

**The row.** `TunnelRow` calls `useSortable({ id: tunnel.id })`.

- `setNodeRef` and the transform style go on the `<li>`.
- `attributes` and `listeners` go on the **handle**, so keyboard focus lands there.
- Handle is a `<button type="button">` with `<GripVertical className="h-4 w-4" />` (lucide is already a dependency), `aria-label={`Reorder ${tunnel.name}`}`, `cursor-grab active:cursor-grabbing`, muted foreground, full opacity on hover/focus.
- While `isDragging`: raised `z-index`, subtle shadow, `bg-background` so the lifted row reads as detached from the divided list.

**Layout.** The `<li>` currently *is* the content flex container (`flex flex-col gap-4 py-5 sm:flex-row sm:items-center sm:justify-between`, lines 119-124). Introduce one wrapper so the handle sits beside that container rather than inside its stacking flow:

```
<li className="flex items-start gap-3 py-5 [+ active bg]">
  <button {...attributes} {...listeners} className="self-start mt-0.5 ..." />
  <div className="min-w-0 flex-1 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
    ... existing row content, unchanged ...
  </div>
</li>
```

`py-5` and the `bg-primary/[0.03]` active highlight move to the `<li>`; the wrapper takes the rest of the original classes plus `min-w-0 flex-1` so the existing truncation behavior is preserved. Mobile stacking is unchanged, and `self-start` keeps the handle aligned with the tunnel name rather than floating in the middle of a stacked card.

**One conflict to fix:** the endpoint anchor at lines 139-147 needs `draggable={false}`. Browsers natively drag links, which would compete with the sortable.

### Demo mode

Demo mode swaps in synthetic tunnels with different ids (`isDemoMode` in `tunnelStore`). If reconciliation ran during demo, it would see all real tunnels as absent and **prune the user's real order**. So the effect returns early when `isDemoMode` is true.

Dragging still works in demo (the store is shared), and any demo ids that get persisted are harmless — they're stale entries that `orderTunnels` skips and the next real reconcile prunes.

This is also the mode most likely to give false confidence: demo disables the list query entirely (`queries.ts:26`), so nothing clobbers the array. **A naive implementation appears to work perfectly in demo and breaks in live mode.** Manual verification must be done with the server running.

## Testing

Add `vitest` as a devDependency and a `"test": "vitest"` script. Vitest reads the existing `vite.config.ts`, so config is a small `test` block plus a `/// <reference types="vitest" />`. No jsdom, no testing-library — the unit under test is a pure function.

`web/src/lib/tunnelOrder.test.ts`:

| Case | Asserts |
|---|---|
| saved order respected | output follows `saved` sequence, not name order |
| unknown prepended | a tunnel absent from `saved` lands at index 0 |
| multiple unknowns | prepended sorted A→Z, not in input order |
| empty saved list | output is alphabetical (first-run seed) |
| stale ids ignored | `orderTunnels` skips saved ids with no tunnel |
| deleted ids pruned | `reconcileOrder` drops them from the persisted array |
| stable input | `reconcileOrder` returns `null` — guards the effect loop |
| random input order | same output regardless of input array order (guards constraint 2) |

The drag interaction itself is verified in the browser, not jsdom. Pointer-event simulation in jsdom is unreliable enough that such tests mostly assert on mocks.

**Manual verification checklist** (live mode, server running):

1. Drag a row; it moves and the rest shift to make room.
2. Wait >5s through a poll; order holds.
3. Reload the page; order holds.
4. Start/stop a tunnel; order holds.
5. Create a tunnel via `tunnelctl`; it appears at top within 5s and stays put afterwards.
6. Delete a tunnel; the rest keep their relative order.
7. Tab to a handle, space to lift, arrows to move, space to drop.
8. Drag on a narrow viewport (stacked layout).

## Dependencies

```
@dnd-kit/core       ^6.3.1   peer: react >=16.8.0  ✓ React 19.2
@dnd-kit/sortable   ^10.0.0
@dnd-kit/modifiers            vertical-axis + parent constraint
vitest              devDep
```

Peer ranges verified against the registry — installs clean on React 19.2 with no `--legacy-peer-deps`.

## Adjacent issues found, deliberately not fixed

Recorded so they aren't lost, but each is its own change:

- **`Manager.List()` returns random order** (`internal/tunnel/manager.go:467`). The root cause this feature works around. Worth sorting server-side regardless.
- **`setTunnels` called during render** (`web/src/lib/queries.ts:30`). React anti-pattern; our effect works around it.
- **Delete has no confirmation** (`TunnelList.tsx:190`) — `deleteTunnel.mutate` fires directly. One misclick destroys a tunnel.
- **`TunnelForm.tsx:141-152` hardcodes `keepAlive: 30` and `maxRetries: 5`** on every update, silently discarding custom values.
- **CLAUDE.md is wrong on two counts**: it specifies PostgreSQL (the project uses SQLite via `modernc.org/sqlite`) and Ant Design (the project uses Radix + Tailwind).
- **`api/openapi.yaml` is stale** — documents only `get`/`delete` on `/tunnels/{id}`, omitting the existing `PUT`.

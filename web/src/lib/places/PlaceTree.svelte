<script lang="ts">
  import { tick, untrack } from 'svelte'
  import Icon from '../Icon.svelte'
  import type { TreePlace } from '../inventory/types'
  import { canDropItems, canDropPlace, edgeAt, treeKey, type Edge } from './logic'
  import { drag, endDrag, startDrag } from './drag.svelte'
  import { kindIcon, roomColor } from './kinds'
  import type { PlacesStore } from './store.svelte'

  interface Props {
    store: PlacesStore
    selectedId: string | null
    canWrite: boolean
    /** Phone: taller rows, no dragging. */
    phone?: boolean
    onselect: (id: string) => void
    onrename: (id: string, name: string) => void
    onreorder: (id: string, step: -1 | 1) => void
    /** Keyboard path to "Move to…" (the M key). */
    onmove: (id: string) => void
    ondropplace: (id: string, target: string, edge: Edge) => void
    ondropitems: (target: string) => void
  }
  let { store, selectedId, canWrite, phone = false, onselect, onrename, onreorder, onmove, ondropplace, ondropitems }: Props = $props()

  let focusId = $state<string | null>(null)
  let renaming = $state<string | null>(null)
  let draft = $state('')
  let hint = $state<{ id: string; edge: Edge } | null>(null)
  let root = $state<HTMLElement>()
  const current = $derived(store.visible.some((r) => r.place.id === focusId) ? focusId : (selectedId && store.visible.some((r) => r.place.id === selectedId) ? selectedId : (store.visible[0]?.place.id ?? null)))

  const known = $derived(!!selectedId && store.byId.has(selectedId)) // true again once the tree has loaded

  // Bring the selected place into view (after the palette or a link opened it).
  $effect(() => {
    const id = selectedId
    if (!id || !root || !known) return
    untrack(() => store.reveal(id))
    tick().then(() => root?.querySelector<HTMLElement>(`[data-id="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'nearest' }))
  })

  async function focusRow(id: string) {
    focusId = id
    await tick()
    root?.querySelector<HTMLElement>(`[data-id="${CSS.escape(id)}"] button.node`)?.focus()
  }

  function onkeydown(e: KeyboardEvent) {
    if (renaming || e.metaKey || e.ctrlKey) return
    if (e.key === 'F2' && canWrite && current) {
      e.preventDefault()
      startRename(current)
      return
    }
    if ((e.key === 'm' || e.key === 'M') && !e.altKey && canWrite && current) {
      e.preventDefault()
      onmove(current)
      return
    }
    const r = treeKey(store.visible, store.expanded, current, e.key, e.altKey)
    if (!Object.keys(r).length) return
    e.preventDefault()
    if (r.focus) focusRow(r.focus)
    if (r.open) store.toggle(r.open.id, r.open.value)
    if (r.select) onselect(r.select)
    if (r.reorder && canWrite) onreorder(r.reorder.id, r.reorder.step)
  }

  function startRename(id: string) {
    renaming = id
    draft = store.byId.get(id)?.name ?? ''
    tick().then(() => root?.querySelector<HTMLInputElement>('input.rename')?.select())
  }
  function endRename(save: boolean) {
    const id = renaming
    renaming = null
    if (id && save && draft.trim() && draft.trim() !== store.byId.get(id)?.name) onrename(id, draft.trim())
    if (id) focusRow(id)
  }

  function over(e: DragEvent, p: TreePlace) {
    const w = drag.what
    if (!w) return
    const el = e.currentTarget as HTMLElement
    let edge: Edge = 'inside'
    let ok = false
    if (w.kind === 'place') {
      edge = edgeAt(e.clientY - el.getBoundingClientRect().top, el.offsetHeight)
      ok = canDropPlace(store.places, w.id, p.id, edge)
    } else ok = canDropItems(w.items, p.id)
    if (!ok) return (hint = null)
    e.preventDefault()
    hint = { id: p.id, edge }
  }
  function drop(e: DragEvent, p: TreePlace) {
    const w = drag.what
    const h = hint
    hint = null
    if (!w || !h) return
    e.preventDefault()
    if (w.kind === 'place') ondropplace(w.id, p.id, h.edge)
    else ondropitems(p.id)
  }
  // Hovering a collapsed node while dragging opens it after a moment.
  let hoverTimer: ReturnType<typeof setTimeout> | undefined
  function hoverOpen(p: TreePlace, hasChildren: boolean) {
    clearTimeout(hoverTimer)
    if (hasChildren && !store.expanded.has(p.id)) hoverTimer = setTimeout(() => store.toggle(p.id, true), 600)
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div bind:this={root} role="tree" aria-label="Places" tabindex="-1" {onkeydown} class="flex flex-col py-1.5 {phone ? '' : 'px-2'}">
  {#each store.visible as r (r.place.id)}
    {@const p = r.place}
    {@const sel = p.id === selectedId}
    {@const dropping = hint?.id === p.id}
    <div
      data-id={p.id}
      role="treeitem"
      aria-label={p.name}
      tabindex="-1"
      aria-selected={sel}
      aria-level={r.depth + 1}
      aria-expanded={r.hasChildren ? store.expanded.has(p.id) : undefined}
      draggable={canWrite && !phone && renaming !== p.id}
      ondragstart={(e) => startDrag({ kind: 'place', id: p.id }, e)}
      ondragend={() => ((hint = null), endDrag())}
      ondragover={(e) => (over(e, p), hoverOpen(p, r.hasChildren))}
      ondragleave={() => hint?.id === p.id && (hint = null)}
      ondrop={(e) => drop(e, p)}
      class="relative flex items-center rounded-control {phone ? 'min-h-12' : 'min-h-9'} {sel ? 'bg-accent-row' : 'hover:bg-chrome-hover'} {dropping && hint?.edge === 'inside' ? 'bg-accent-soft outline-2 -outline-offset-2 outline-accent' : ''}"
      style:padding-left="{r.depth * (phone ? 18 : 14) + (phone ? 8 : 0)}px"
    >
      {#if dropping && hint?.edge === 'before'}<span class="pointer-events-none absolute inset-x-1 top-0 h-0.5 rounded bg-accent"></span>{/if}
      {#if dropping && hint?.edge === 'after'}<span class="pointer-events-none absolute inset-x-1 bottom-0 h-0.5 rounded bg-accent"></span>{/if}
      {#if r.hasChildren}
        <button type="button" tabindex="-1" onclick={() => store.toggle(p.id)} aria-label="{store.expanded.has(p.id) ? 'Collapse' : 'Expand'} {p.name}" class="flex flex-none cursor-pointer items-center justify-center rounded-control text-muted hover:bg-chrome-hover {phone ? 'size-11' : 'size-7'}">
          <Icon name={store.expanded.has(p.id) ? 'chevron-down' : 'chevron-right'} size={14} />
        </button>
      {:else}
        <span class="flex-none {phone ? 'size-11' : 'size-7'}"></span>
      {/if}
      {#if renaming === p.id}
        <Icon name={kindIcon[p.kind] ?? 'pin'} class="mr-2 text-label" />
        <input
          class="rename h-8 min-w-0 flex-1 rounded-control border border-accent bg-surface px-2 text-[14px] outline-0"
          bind:value={draft}
          aria-label="Rename {p.name}"
          onkeydown={(e) => {
            e.stopPropagation()
            if (e.key === 'Enter') endRename(true)
            else if (e.key === 'Escape') endRename(false)
          }}
          onblur={() => renaming && endRename(true)}
        />
      {:else}
        <button
          type="button"
          class="node flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-1 pr-2 text-left text-[14px] {sel ? 'font-semibold' : ''}"
          tabindex={p.id === current ? 0 : -1}
          onfocus={() => (focusId = p.id)}
          onclick={() => onselect(p.id)}
          ondblclick={() => canWrite && !phone && startRename(p.id)}
        >
          {#if p.kind === 'room' && p.color && roomColor[p.color]}
            <span class="size-2.5 flex-none rounded-full" style:background={roomColor[p.color]} title="{p.color} room"></span>
          {:else}
            <Icon name={kindIcon[p.kind] ?? 'pin'} class="text-label" />
          {/if}
          <span class="truncate">{p.name}</span>
          <span class="ml-auto flex-none font-mono text-[12px] text-muted" title="Items here and below">{p.counts?.items_below ?? 0}</span>
        </button>
      {/if}
    </div>
  {/each}
</div>

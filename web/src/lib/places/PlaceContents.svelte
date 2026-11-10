<script lang="ts">
  import { tick, untrack } from 'svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import { valueLabel } from '../inventory/categories'
  import ItemRow from '../inventory/ItemRow.svelte'
  import type { ItemList } from '../inventory/list.svelte'
  import { bulkItems } from '../inventory/items'
  import PlaceDialog from '../inventory/PlaceDialog.svelte'
  import { createLine, deleteLine, listLines, patchLine, promoteLine, restoreLine } from '../inventory/places'
  import type { Catalog, ContentsLine, Item, PlaceShort, TreePlace } from '../inventory/types'
  import SectionBar from '../shell/SectionBar.svelte'
  import Toolbar from '../shell/Toolbar.svelte'
  import Button from '../ui/Button.svelte'
  import Popover from '../ui/Popover.svelte'
  import StatusPill from '../ui/StatusPill.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { endDrag, startDrag } from './drag.svelte'

  interface Props {
    place: TreePlace
    list: ItemList
    catalog: Catalog | null
    canWrite: boolean
    phone?: boolean
    openItemId: string | null
    highlightLine: string | null
    onopenitem: (id: string) => void
    /** Moves items (null = not placed); the screen does the call, the counts and the undo. */
    onmoveitems: (items: Item[], target: PlaceShort | null) => void
    /** Lines or items were added, removed or promoted: the counts changed. */
    onchanged: () => void
  }
  let { place, list, catalog, canWrite, phone = false, openItemId, highlightLine, onopenitem, onmoveitems, onchanged }: Props = $props()

  // ---- items ----
  // An item moved elsewhere from the detail panel leaves the table at once.
  const items = $derived(list.items.filter((i) => i.place?.id === place.id))

  let selectMode = $state(false)
  let selected = $state.raw(new Set<string>())
  let moveOpen = $state(false)
  let statusOpen = $state(false)
  const chosen = $derived(items.filter((i) => selected.has(i.id)))
  function toggleSelect(id: string) {
    const next = new Set(selected)
    if (!next.delete(id)) next.add(id)
    selected = next
  }
  function endSelect() {
    selectMode = false
    selected = new Set()
  }
  async function setStatus(s: string) {
    const ids = chosen.map((i) => i.id)
    try {
      for (let i = 0; i < ids.length; i += 500) list.replace(...(await bulkItems(ids.slice(i, i + 500), { status: s })).data)
      showToast(`Status set to ${valueLabel(s)} on ${ids.length} items`, 'success')
      endSelect()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }

  function dragItem(e: DragEvent, item: Item) {
    const group = selectMode && selected.has(item.id) ? chosen : [item]
    startDrag({ kind: 'items', items: group }, e)
  }

  // ---- lines ----
  let lines = $state<ContentsLine[]>([])
  let linesError = $state('')
  let linesFor = ''
  $effect(() => {
    const id = place.id
    untrack(() => {
      lines = []
      linesError = ''
      endSelect()
    })
    linesFor = id
    listLines(id).then(
      async (p) => {
        if (id !== linesFor) return
        lines = p.data
        if (highlightLine) {
          await tick()
          document.getElementById(`line-${highlightLine}`)?.scrollIntoView({ block: 'center' })
        }
      },
      (e) => {
        if (id === linesFor) linesError = explain(e).message
      },
    )
  })

  let newName = $state('')
  let newQty = $state('1')
  let adding = $state(false)
  async function addLine(e: SubmitEvent) {
    e.preventDefault()
    const name = newName.trim()
    if (!name || adding) return
    adding = true
    try {
      const made = (await createLine(place.id, name, Math.max(0, Math.floor(Number(newQty) || 1)))).data
      lines = [...lines, ...made]
      newName = ''
      newQty = '1'
      onchanged()
    } catch (err) {
      showToast(explain(err).message || Object.values(explain(err).fields)[0] || 'Could not add that.', 'error')
    } finally {
      adding = false
    }
  }

  let editing = $state<string | null>(null)
  let nameDraft = $state('')
  let qtyDraft = $state('1')
  function edit(l: ContentsLine) {
    editing = l.id
    nameDraft = l.name
    qtyDraft = String(l.quantity)
    tick().then(() => document.querySelector<HTMLInputElement>('input.line-name')?.select())
  }
  async function saveLine(l: ContentsLine) {
    const name = nameDraft.trim()
    const quantity = Math.max(0, Math.floor(Number(qtyDraft)))
    editing = null
    if (!name || Number.isNaN(quantity) || (name === l.name && quantity === l.quantity)) return
    try {
      const next = await patchLine(l.id, { name, quantity })
      lines = lines.map((x) => (x.id === l.id ? next : x))
    } catch (err) {
      const x = explain(err)
      showToast(x.message || Object.values(x.fields)[0] || 'Could not save that.', 'error')
    }
  }
  async function removeLine(l: ContentsLine) {
    const at = lines.findIndex((x) => x.id === l.id)
    try {
      await deleteLine(l.id)
      lines = lines.filter((x) => x.id !== l.id)
      onchanged()
      showToast(`Removed ${l.name}`, 'info', 7000, {
        label: 'Undo',
        run: async () => {
          try {
            const back = await restoreLine(l.id)
            const next = [...lines]
            next.splice(Math.min(at, next.length), 0, back)
            lines = next
            onchanged()
          } catch (e) {
            showToast(explain(e).message, 'error')
          }
        },
      })
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  async function makeItem(l: ContentsLine) {
    try {
      const it = await promoteLine(l.id)
      lines = lines.filter((x) => x.id !== l.id)
      list.prepend(it)
      onchanged()
      showToast(`${it.name} is now an item`, 'success', 6000, { label: 'Open', run: () => onopenitem(it.id) })
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }

  const iconBtn = 'inline-flex size-11 flex-none cursor-pointer items-center justify-center rounded-control text-muted hover:bg-chrome-hover md:size-8'
</script>

{#if canWrite && selectMode}
  <Toolbar>
    <span class="text-[13px] font-semibold">{selected.size} selected</span>
    <Button icon="move" disabled={!selected.size} onclick={() => (moveOpen = true)}>Move</Button>
    <Popover bind:open={statusOpen}>
      {#snippet trigger({ toggle, open })}
        <Button icon="pulse" disabled={!selected.size || !catalog} onclick={toggle} aria-expanded={open}>Set status<Icon name="chevron-down" size={13} /></Button>
      {/snippet}
      {#each catalog?.statuses ?? [] as s (s)}
        <button type="button" class="flex h-11 w-full cursor-pointer items-center rounded-control px-2.5 hover:bg-chrome-hover md:h-9" onclick={() => ((statusOpen = false), setStatus(s))}>
          <StatusPill status={s} />
        </button>
      {/each}
    </Popover>
    <Button variant="ghost" onclick={() => (selected = new Set(items.map((i) => i.id)))}>Select all</Button>
    <Button variant="ghost" class="ml-auto" onclick={endSelect}>Done</Button>
  </Toolbar>
{/if}

<SectionBar icon="tag" title="Items">
  <span class="font-mono text-muted">{items.length}{list.next ? '+' : ''}</span>
  {#if canWrite && items.length && !selectMode}
    <Button class="h-8" icon="check" onclick={() => (selectMode = true)}>Select</Button>
  {/if}
</SectionBar>
<div class="@container bg-surface">
  {#if list.error}
    <p role="alert" class="m-0 px-4 py-3 text-[13px] text-danger md:px-6">{list.error}</p>
  {:else if !list.loaded}
    <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">Loading</p>
  {:else if !items.length}
    <p class="m-0 px-4 py-4 text-[13px] text-muted md:px-6">No items are directly in {place.name}.{#if canWrite} Drop items from the list here, or capture new ones.{/if}</p>
  {/if}
  {#each items as item (item.id)}
    <div role="listitem" draggable={canWrite && !phone} ondragstart={(e) => dragItem(e, item)} ondragend={endDrag}>
      <ItemRow {item} open={item.id === openItemId} {selectMode} checked={selected.has(item.id)} ontoggle={() => toggleSelect(item.id)} onopen={() => onopenitem(item.id)} />
    </div>
  {/each}
  {#if list.next}
    <div class="border-b border-divider-row px-4 py-2 md:px-6"><Button onclick={() => list.more()} disabled={list.loading}>{list.loading ? 'Loading' : 'Load more'}</Button></div>
  {/if}
</div>

<SectionBar icon="package" title="Loose contents">
  <span class="font-mono text-muted">{lines.length}</span>
</SectionBar>
<div class="bg-surface">
  {#if linesError}<p role="alert" class="m-0 px-4 py-3 text-[13px] text-danger md:px-6">{linesError}</p>{/if}
  {#if !lines.length && !linesError}
    <p class="m-0 px-4 py-4 text-[13px] text-muted md:px-6">Cheap or bulky things (cables, screws, socks) can be listed here with a quantity instead of one item each.</p>
  {/if}
  {#each lines as l (l.id)}
    <div id="line-{l.id}" class="flex min-h-14 items-center gap-2 border-b border-divider-row px-4 md:min-h-12 md:px-6 {l.id === highlightLine ? 'bg-ai-bg' : ''}">
      <span class="flex size-8 flex-none items-center justify-center rounded-[9px] bg-stored-bg text-stored-fg"><Icon name="package" size={15} /></span>
      {#if editing === l.id}
        <form class="flex min-w-0 flex-1 items-center gap-2" onsubmit={(e) => (e.preventDefault(), saveLine(l))}>
          <input class="line-name h-11 min-w-0 flex-1 rounded-control border border-accent bg-surface px-2 text-[16px] md:h-8 md:text-[14px]" bind:value={nameDraft} aria-label="Name" onkeydown={(e) => e.key === 'Escape' && (editing = null)} />
          <input class="h-11 w-20 rounded-control border border-border-input bg-surface px-2 text-right font-mono text-[16px] md:h-8 md:text-[14px]" type="number" min="0" bind:value={qtyDraft} aria-label="Quantity" onkeydown={(e) => e.key === 'Escape' && (editing = null)} />
          <button type="submit" class={iconBtn} aria-label="Save"><Icon name="check" /></button>
          <button type="button" class={iconBtn} aria-label="Cancel" onclick={() => (editing = null)}><Icon name="x" /></button>
        </form>
      {:else}
        <button type="button" disabled={!canWrite} onclick={() => edit(l)} class="flex min-w-0 flex-1 cursor-pointer items-center gap-2 py-2 text-left disabled:cursor-default" title={canWrite ? 'Edit' : undefined}>
          <span class="truncate text-[14px] font-semibold">{l.name}</span>
          {#if l.ai}<span class="flex-none text-ai-fg" title="Read from a photo by AI"><Icon name="sparkle" size={13} /></span>{/if}
          <span class="flex-none font-mono text-[12px] text-muted">×{l.quantity}</span>
        </button>
        {#if canWrite}
          <Button class="h-11 flex-none md:h-8" icon="tag" onclick={() => makeItem(l)}>Make item</Button>
          <button type="button" class={iconBtn} aria-label="Remove {l.name}" onclick={() => removeLine(l)}><Icon name="trash" /></button>
        {/if}
      {/if}
    </div>
  {/each}
  {#if canWrite}
    <form class="flex items-center gap-2 px-4 py-3 md:px-6" onsubmit={addLine}>
      <input bind:value={newName} placeholder="Add a line, e.g. USB cables" aria-label="New line" class="h-11 min-w-0 flex-1 rounded-control border border-border-input bg-surface px-3 text-[16px] md:h-9 md:text-[14px]" />
      <input bind:value={newQty} type="number" min="0" aria-label="Quantity" class="h-11 w-20 rounded-control border border-border-input bg-surface px-2 text-right font-mono text-[16px] md:h-9 md:text-[14px]" />
      <Button type="submit" icon="plus" disabled={!newName.trim() || adding}>Add</Button>
    </form>
  {/if}
</div>

<PlaceDialog bind:open={moveOpen} title="Move {selected.size} {selected.size === 1 ? 'item' : 'items'}" allowNone onpick={(p) => (onmoveitems(chosen, p), endSelect())} />

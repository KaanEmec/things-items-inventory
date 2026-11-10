<script lang="ts">
  import { untrack } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'
  import { api } from '../lib/api'
  import { activeHousehold, can } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import Icon from '../lib/Icon.svelte'
  import type { IconName } from '../lib/icons'
  import { bulkItems, createItem } from '../lib/inventory/items'
  import { getCatalog, valueLabel } from '../lib/inventory/categories'
  import ItemDetail from '../lib/inventory/ItemDetail.svelte'
  import ItemForm from '../lib/inventory/ItemForm.svelte'
  import ItemRow from '../lib/inventory/ItemRow.svelte'
  import { ItemList } from '../lib/inventory/list.svelte'
  import { listAction } from '../lib/inventory/keys'
  import PlaceDialog from '../lib/inventory/PlaceDialog.svelte'
  import type { Catalog, Item, PlaceShort } from '../lib/inventory/types'
  import { scrollToRow, windowRange } from '../lib/inventory/window'
  import { navigate, router } from '../lib/router.svelte'
  import { stream } from '../lib/stream.svelte'
  import { filterCount, filtersToSearch, parseFilters, toQuery, type Filters } from '../lib/search/filters'
  import { canFireShortcut, dialogIsOpen } from '../lib/search/guard'
  import { palette } from '../lib/search/state.svelte'
  import SearchField from '../lib/search/SearchField.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import PanelFooter from '../lib/shell/PanelFooter.svelte'
  import Toolbar from '../lib/shell/Toolbar.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Dialog from '../lib/ui/Dialog.svelte'
  import FormError from '../lib/ui/FormError.svelte'
  import Popover from '../lib/ui/Popover.svelte'
  import StatusPill from '../lib/ui/StatusPill.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  const list = new ItemList()
  const docked = new MediaQuery('min-width: 768px')
  let catalog = $state<Catalog | null>(null)
  let catalogError = $state('')

  getCatalog().then(
    (c) => (catalog = c),
    (e) => (catalogError = explain(e).message),
  )

  // ---- filters ----
  const chips: { key: string; label: string; icon: IconName }[] = [
    { key: 'gadget', label: 'Gadgets', icon: 'device' },
    { key: 'clothing', label: 'Clothing', icon: 'shirt' },
    { key: 'accessory', label: 'Accessories', icon: 'watch' },
    { key: 'part', label: 'Parts', icon: 'chip' },
    { key: 'other', label: 'Other', icon: 'package' },
  ]
  const filters = $derived(parseFilters(router.search))
  const filterKey = $derived(filtersToSearch(filters))
  const filtered = $derived(filterCount(filters) > 0)
  let statusFilterOpen = $state(false)
  let placeFilterOpen = $state(false)
  let placeFilter = $state<PlaceShort | null>(null)

  /** Changes filters by rewriting the URL query; the list follows from it. Other parameters are dropped. */
  function setFilters(patch: Partial<Filters>) {
    navigate(`${router.path}${filtersToSearch({ ...filters, ...patch })}`, true)
  }
  const { category, statuses, sort } = $derived(filters)

  $effect(() => {
    filterKey
    untrack(() => list.reset(toQuery(filters)))
  })

  // Live: another member or a background job changed something.
  $effect(() => stream.onBurst(['item', 'box', 'place', 'job', 'resync'], () => list.refresh()))

  // The chip needs the place's name; the URL only holds its id.
  $effect(() => {
    const id = filters.placeId
    if (!id) return void (placeFilter = null)
    if (untrack(() => placeFilter?.id) === id) return
    let live = true
    api.get<PlaceShort>(`/api/v1/places/${id}`).then(
      (p) => live && (placeFilter = p),
      () => live && (placeFilter = { id, kind: 'spot', name: 'Place', path: '', home_id: null, box_number: null, color: null }),
    )
    return () => (live = false)
  })

  // "?new=1" (palette action) opens the new-item dialog once.
  $effect(() => {
    if (new URLSearchParams(router.search).get('new') === '1') {
      untrack(() => (newOpen = true))
      navigate(`${router.path}${filterKey}`, true)
    }
  })

  const shown = $derived(list.items)

  function clearFilters() {
    navigate(router.path, true)
  }

  // ---- selection and keyboard ----
  const openId = $derived(/^\/items\/([^/]+)/.exec(router.path)?.[1] ?? null)
  let cursorId = $state<string | null>(null)
  let selectMode = $state(false)
  let selected = $state.raw(new Set<string>())
  const selectedIds = $derived([...selected])

  function toggleSelect(id: string) {
    const next = new Set(selected)
    if (!next.delete(id)) next.add(id)
    selected = next
  }
  function endSelect() {
    selectMode = false
    selected = new Set()
  }
  function open(id: string) {
    cursorId = id
    navigate(`/items/${id}${router.search}`)
  }

  // ---- windowing ----
  let scroller = $state<HTMLElement>()
  let scrollTop = $state(0)
  let viewH = $state(0)
  let viewW = $state(0)
  const rowH = $derived(viewW >= 520 ? 56 : 64) // matches the h-14 / h-16 of ItemRow's container query
  const range = $derived(windowRange(scrollTop, viewH, rowH, shown.length))

  $effect(() => {
    const need = list.next && !list.loading && range.end >= shown.length - 20
    if (need) untrack(() => list.more())
  })

  function onkeydown(e: KeyboardEvent) {
    const action = listAction(e.key)
    if (!action || !canFireShortcut(e, dialogIsOpen())) return
    if (!docked.current && openId) return // the phone detail covers the list
    const t = e.target as HTMLElement
    if (action === 'open' && t.closest?.('a, button, summary')) return // let the focused control act
    if (action === 'clear') {
      if (selectMode) endSelect()
      return
    }
    const at = shown.findIndex((i) => i.id === (cursorId ?? openId))
    if (action === 'next' || action === 'prev') {
      e.preventDefault()
      const to = Math.min(Math.max(at < 0 ? 0 : at + (action === 'next' ? 1 : -1), 0), shown.length - 1)
      const it = shown[to]
      if (!it) return
      cursorId = it.id
      if (scroller) scroller.scrollTop = scrollToRow(scroller.scrollTop, viewH, rowH, to)
      if (docked.current) navigate(`/items/${it.id}${router.search}`, true)
    } else if (action === 'open') {
      if (cursorId) e.preventDefault(), open(cursorId)
    } else if (action === 'select' && can('member')) {
      const id = cursorId ?? openId
      if (!id) return
      e.preventDefault()
      selectMode = true
      toggleSelect(id)
    }
  }

  // ---- bulk ----
  let bulkMoveOpen = $state(false)
  let bulkStatusOpen = $state(false)
  async function bulk(change: { place_id?: string | null; status?: string }, done: string) {
    const ids = selectedIds
    try {
      for (let i = 0; i < ids.length; i += 500) list.replace(...(await bulkItems(ids.slice(i, i + 500), change)).data)
      showToast(done, 'success')
      endSelect()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }

  // The palette offers "Move selected to…" while items are selected.
  $effect(() => {
    const count = selected.size
    palette.selection = count ? { count, move: () => (bulkMoveOpen = true) } : null
    return () => (palette.selection = null)
  })

  // ---- new item ----
  let newOpen = $state(false)
  async function create(body: Record<string, unknown>) {
    const it: Item = await createItem(body)
    list.prepend(it)
    newOpen = false
    navigate(`/items/${it.id}`)
    showToast(`Added ${it.name}`, 'success')
  }

  const chip =
    'inline-flex h-11 flex-none cursor-pointer items-center gap-1.5 rounded-control border px-3 text-[13px] font-medium md:h-8 md:px-2.5'
  const chipOff = 'border-border-input bg-surface text-body hover:bg-surface-subtle'
  const chipOn = 'border-ink bg-ink text-white'
  const canWrite = $derived(can('member'))
</script>

<svelte:window {onkeydown} />

{#snippet listPane()}
  {#if catalogError || list.error}
    <div class="p-4 md:px-6">
      <FormError message={catalogError || list.error} />
      <Button class="mt-3" icon="refresh" onclick={() => list.reset(list.query)}>Try again</Button>
    </div>
  {:else if list.loaded && !list.items.length && !list.loading}
    <div class="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-12 text-center">
      <span class="flex size-12 items-center justify-center rounded-tile bg-accent-soft text-accent"><Icon name="tag" size={22} /></span>
      {#if filtered}
        <h2 class="m-0 text-[17px] font-bold">Nothing matches</h2>
        <p class="m-0 max-w-[34ch] text-[14px] text-muted">No item fits these filters. Change them or start again.</p>
        <Button onclick={clearFilters}>Clear filters</Button>
      {:else}
        <h2 class="m-0 text-[17px] font-bold">No items yet</h2>
        <p class="m-0 max-w-[40ch] text-[14px] text-muted">Start with a photo: Capture reads it and fills in the details. You can also type a list in Quick add, or add one item by hand.</p>
        <div class="flex flex-wrap justify-center gap-2">
          <Button variant="primary" icon="camera" href="/capture">Capture</Button>
          <Button icon="table" href="/quick-add">Quick add</Button>
          {#if canWrite}<Button icon="plus" onclick={() => (newOpen = true)} disabled={!catalog}>New item</Button>{/if}
        </div>
      {/if}
    </div>
  {:else}
    <div
      bind:this={scroller}
      bind:clientHeight={viewH}
      bind:clientWidth={viewW}
      onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
      class="@container min-h-0 flex-1 overflow-y-auto bg-surface max-md:border-y max-md:border-divider"
    >
      <div class="relative" style:height="{shown.length * rowH}px" role="list" aria-label="Items">
        {#each shown.slice(range.start, range.end) as item, k (item.id)}
          <div class="absolute inset-x-0" style:top="{(range.start + k) * rowH}px" role="listitem">
            <ItemRow {item} open={docked.current && item.id === openId} cursor={item.id === cursorId && item.id !== openId} {selectMode} checked={selected.has(item.id)} ontoggle={() => toggleSelect(item.id)} />
          </div>
        {/each}
      </div>
      {#if list.loading}<p class="m-0 px-6 py-3 text-[13px] text-muted">Loading</p>{/if}
    </div>
  {/if}
{/snippet}

<Panel>
  <PageHeader icon="tag" title="Items" breadcrumb={activeHousehold() ? [activeHousehold()!.name] : undefined}>
    {#snippet actions()}
      <span class="hidden font-mono text-[13px] text-muted md:inline">{list.items.length}{list.next ? '+' : ''}</span>
      <Button href="/quick-add" icon="table" class="max-md:hidden">Quick add</Button>
      {#if canWrite}<Button variant="primary" icon="plus" onclick={() => (newOpen = true)} disabled={!catalog}>New item</Button>{/if}
    {/snippet}
  </PageHeader>

  <Toolbar>
    <SearchField placeholder="Search items…" class="min-w-0 flex-[1_1_100%] md:flex-[0_1_240px]" />
    <div class="flex min-w-0 flex-[1_1_100%] gap-1.5 overflow-x-auto md:flex-[0_1_auto] md:flex-wrap md:overflow-visible">
      <button type="button" class="{chip} {category === '' ? chipOn : chipOff}" onclick={() => setFilters({ category: '' })}>All</button>
      {#each chips as c (c.key)}
        <button type="button" class="{chip} {category === c.key ? chipOn : chipOff}" aria-pressed={category === c.key} onclick={() => setFilters({ category: category === c.key ? '' : c.key })}>
          <Icon name={c.icon} class={category === c.key ? '' : 'text-muted'} />{c.label}
        </button>
      {/each}
    </div>
    <span class="mx-1 hidden h-5 w-px bg-border-input md:block"></span>
    <Popover bind:open={statusFilterOpen}>
      {#snippet trigger({ toggle, open })}
        <button type="button" class="{chip} {statuses.length ? chipOn : chipOff}" onclick={toggle} aria-expanded={open}>
          <Icon name="pulse" class={statuses.length ? '' : 'text-muted'} />Status{statuses.length ? ` · ${statuses.length}` : ''}<Icon name="chevron-down" size={13} />
        </button>
      {/snippet}
      {#each catalog?.statuses ?? [] as s (s)}
        <label class="flex h-11 cursor-pointer items-center gap-2.5 rounded-control px-2.5 text-[14px] hover:bg-chrome-hover md:h-9">
          <input type="checkbox" class="accent-accent" checked={statuses.includes(s)} onchange={(e) => setFilters({ statuses: e.currentTarget.checked ? [...statuses, s] : statuses.filter((x) => x !== s) })} />
          <StatusPill status={s} />
        </label>
      {/each}
    </Popover>
    <button type="button" class="{chip} {filters.placeId ? chipOn : chipOff}" onclick={() => (placeFilterOpen = true)}>
      <Icon name="pin" class={filters.placeId ? '' : 'text-muted'} /><span class="max-w-40 truncate">{filters.placeId ? (placeFilter?.name ?? 'Place') : 'Place'}</span>
    </button>
    <button type="button" class="{chip} {filters.draft ? chipOn : chipOff}" aria-pressed={filters.draft} onclick={() => setFilters({ draft: !filters.draft })}>
      <Icon name="edit" class={filters.draft ? '' : 'text-muted'} />Drafts
    </button>
    <button type="button" class="{chip} {filters.warranty ? chipOn : chipOff}" aria-pressed={filters.warranty} onclick={() => setFilters({ warranty: !filters.warranty })}>
      <Icon name="receipt" class={filters.warranty ? '' : 'text-muted'} />Has warranty
    </button>
    <select value={sort} onchange={(e) => setFilters({ sort: e.currentTarget.value as Filters['sort'] })} aria-label="Sort" class="h-11 rounded-control border border-border-input bg-surface px-2 text-[14px] md:h-8 md:text-[13px]">
      <option value="touched">Recently changed</option>
      <option value="name">Name</option>
      <option value="place">Place</option>
    </select>
    {#if filtered}
      <Button variant="ghost" onclick={clearFilters}>Clear</Button>
    {/if}
    {#if canWrite}
      <Button class="md:ml-auto" icon="check" onclick={() => (selectMode ? endSelect() : (selectMode = true))} aria-pressed={selectMode}>{selectMode ? 'Done' : 'Select'}</Button>
    {/if}
  </Toolbar>

  {#if selectMode}
    <Toolbar>
      <span class="text-[13px] font-semibold">{selected.size} selected</span>
      <Button icon="move" disabled={!selected.size} onclick={() => (bulkMoveOpen = true)}>Move</Button>
      <Popover bind:open={bulkStatusOpen}>
        {#snippet trigger({ toggle, open })}
          <Button icon="pulse" disabled={!selected.size} onclick={toggle} aria-expanded={open}>Set status<Icon name="chevron-down" size={13} /></Button>
        {/snippet}
        {#each catalog?.statuses ?? [] as s (s)}
          <button type="button" class="flex h-11 w-full cursor-pointer items-center rounded-control px-2.5 hover:bg-chrome-hover md:h-9" onclick={() => ((bulkStatusOpen = false), bulk({ status: s }, `Status set to ${valueLabel(s)} on ${selected.size} items`))}>
            <StatusPill status={s} />
          </button>
        {/each}
      </Popover>
      <Button variant="ghost" onclick={() => (selected = new Set(shown.map((i) => i.id)))}>Select all loaded</Button>
    </Toolbar>
  {/if}


  {#if docked.current}
    <div class="flex min-h-0 flex-1">
      <section aria-label="Item list" class="flex min-w-0 flex-[1.25_1_300px] flex-col border-r border-divider">{@render listPane()}</section>
      <aside aria-label="Item detail" class="flex min-w-0 flex-[1_1_340px] flex-col">
        {#if openId && catalog}
          <ItemDetail id={openId} {list} {catalog} />
        {:else}
          <div class="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-center text-muted">
            <Icon name="tag" size={22} />
            <p class="m-0 text-[14px]">{list.items.length ? 'Pick an item to see it here.' : 'Items you add show up here.'}</p>
          </div>
        {/if}
      </aside>
    </div>
    <PanelFooter>
      <span>{shown.length} {shown.length === 1 ? 'item' : 'items'}{list.next ? ', more load as you scroll' : ''}</span>
      <span class="ml-auto font-mono text-[11px]"><kbd class="inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px]">J</kbd> <kbd class="inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px]">K</kbd> move · <kbd class="inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px]">Enter</kbd> open · <kbd class="inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px]">X</kbd> select</span>
    </PanelFooter>
  {:else}
    <div class="flex min-h-0 flex-1 flex-col bg-surface-sunken">{@render listPane()}</div>
    {#if openId && catalog}
      <ItemDetail id={openId} {list} {catalog} phone />
    {/if}
  {/if}
</Panel>

<PlaceDialog bind:open={placeFilterOpen} title="Show items in a place" value={placeFilter} allowNone onpick={(p) => ((placeFilter = p), setFilters({ placeId: p?.id ?? '' }))} />
<PlaceDialog bind:open={bulkMoveOpen} title="Move {selected.size} {selected.size === 1 ? 'item' : 'items'}" allowNone onpick={(p) => bulk({ place_id: p?.id ?? null }, p ? `Moved ${selected.size} items to ${p.name}` : `${selected.size} items marked as not placed`)} />

<Dialog bind:open={newOpen} title="New item" size="lg">
  {#if newOpen && catalog}<ItemForm id="item-new-form" {catalog} place={placeFilter} save={create} />{/if}
  {#snippet actions()}
    <Button onclick={() => (newOpen = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="item-new-form">Add item</Button>
  {/snippet}
</Dialog>

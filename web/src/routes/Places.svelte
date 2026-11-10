<script lang="ts">
  import { untrack } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'
  import { api } from '../lib/api'
  import { activeHousehold, can } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import Icon from '../lib/Icon.svelte'
  import { getCatalog } from '../lib/inventory/categories'
  import { bulkItems } from '../lib/inventory/items'
  import ItemDetail from '../lib/inventory/ItemDetail.svelte'
  import { ItemList } from '../lib/inventory/list.svelte'
  import InBoxes from '../lib/moving/InBoxes.svelte'
  import PlaceDialog from '../lib/inventory/PlaceDialog.svelte'
  import { cachedPlacePhoto, getPlace, patchLine, uploadPlacePhoto, type PlaceKind } from '../lib/inventory/places'
  import { toJpeg } from '../lib/inventory/photos'
  import type { Catalog, Item, PlaceShort, TreePlace } from '../lib/inventory/types'
  import AddPlacesDialog from '../lib/places/AddPlacesDialog.svelte'
  import { drag } from '../lib/places/drag.svelte'
  import { kindIcon } from '../lib/places/kinds'
  import { childKind, dropPosition, expandSpots, kindWord, notEmptyCounts, type Edge } from '../lib/places/logic'
  import PlaceContents from '../lib/places/PlaceContents.svelte'
  import PlaceHeader from '../lib/places/PlaceHeader.svelte'
  import PlaceTree from '../lib/places/PlaceTree.svelte'
  import SpotsStrip from '../lib/places/SpotsStrip.svelte'
  import { failure, isNotEmpty, PlacesStore } from '../lib/places/store.svelte'
  import { navigate, router } from '../lib/router.svelte'
  import { stream } from '../lib/stream.svelte'
  import FlowFrame from '../lib/shell/FlowFrame.svelte'
  import Group from '../lib/shell/Group.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import PanelFooter from '../lib/shell/PanelFooter.svelte'
  import SectionBar from '../lib/shell/SectionBar.svelte'
  import Toolbar from '../lib/shell/Toolbar.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import Skeleton from '../lib/ui/Skeleton.svelte'
  import Dialog from '../lib/ui/Dialog.svelte'
  import Field from '../lib/ui/Field.svelte'
  import Popover from '../lib/ui/Popover.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  const store = new PlacesStore()
  const list = new ItemList()
  const docked = new MediaQuery('min-width: 768px')
  const canWrite = $derived(can('member'))
  let catalog = $state<Catalog | null>(null)
  getCatalog().then((c) => (catalog = c), () => {})

  // ---- URL: ?place=<id>[&line=<id>], #item=<id> ----
  const params = $derived(new URLSearchParams(router.search))
  const selectedId = $derived(params.get('place'))
  const lineParam = $derived(params.get('line'))
  const openItemId = $derived(/(?:^|&)item=([^&]+)/.exec(router.hash)?.[1] ?? null)
  const place = $derived(selectedId ? (store.byId.get(selectedId) ?? null) : null)
  const url = (id: string | null, line?: string | null) => (id ? `/places?place=${encodeURIComponent(id)}${line ? `&line=${encodeURIComponent(line)}` : ''}` : '/places')
  const select = (id: string) => navigate(url(id))

  $effect(() => {
    untrack(() => store.init(selectedId))
  })
  $effect(() => store.watch())
  $effect(() => stream.onBurst(['item', 'box', 'place', 'job', 'resync'], () => list.refresh()))

  // A contents line opens the place that holds it.
  $effect(() => {
    const line = lineParam
    if (!line || selectedId) return
    // No endpoint reads one line; an empty PATCH returns it unchanged.
    patchLine(line, {}).then(
      (l) => navigate(url(l.place_id, l.id), true),
      (e) => (showToast(failure(e), 'error'), navigate('/places', true)),
    )
  })

  // A place of another home switches to that home; an unknown one goes back to the list.
  let looked = ''
  $effect(() => {
    const id = selectedId
    if (!id || !store.loaded || store.byId.has(id) || looked === id) return
    looked = id
    getPlace(id).then(
      async (p) => {
        if (p.home_id && p.home_id !== store.homeId) await store.switchHome(p.home_id)
        else navigate('/places', true)
      },
      () => (showToast('That place no longer exists.', 'error'), navigate('/places', true)),
    )
  })

  // Desktop shows a place at all times.
  $effect(() => {
    if (docked.current && store.loaded && !selectedId && !lineParam && store.roots.length) navigate(url(store.roots[0].id), true)
  })

  // Items of the selected place.
  $effect(() => {
    const id = selectedId
    if (id) untrack(() => list.reset({ place_id: id }))
  })

  const spots = $derived(place ? store.children(place.id) : [])
  const parentOf = (p: TreePlace) => (p.parent_id ? (store.byId.get(p.parent_id) ?? null) : null)

  // ---- photo ----
  let photos = $state<Record<string, string>>({})
  let photoBusy = $state(false)
  const photoOf = (p: TreePlace) => photos[p.id] ?? p.photo?.sha256 ?? cachedPlacePhoto(p.id)?.sha256 ?? null
  async function setPhoto(p: TreePlace, file: File) {
    photoBusy = true
    try {
      const r = await uploadPlacePhoto(p.id, await toJpeg(file))
      photos = { ...photos, [p.id]: r.sha256 }
    } catch (e) {
      showToast(e instanceof Error && !('status' in e) ? e.message : failure(e), 'error')
    } finally {
      photoBusy = false
    }
  }

  // ---- edits ----
  const guard = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
    } catch (e) {
      showToast(failure(e), 'error')
    }
  }

  let addOpen = $state(false)
  let addParent = $state<TreePlace | null>(null)
  function openAdd(parent: TreePlace | null) {
    addParent = parent
    addOpen = true
  }
  async function addPlaces(kind: PlaceKind, names: string[]) {
    const made = await store.add(addParent?.id ?? null, kind, names)
    showToast(made.length === 1 ? `Added ${made[0].name}` : `Added ${made.length} places`, 'success')
  }

  // The quick input under the place header: a name or a pattern, Enter adds.
  let quick = $state('')
  let quickBusy = $state(false)
  const quickParsed = $derived(expandSpots(quick))
  async function quickAdd(e: SubmitEvent) {
    e.preventDefault()
    if (!place || !quickParsed.names.length || quickBusy) return
    quickBusy = true
    try {
      const made = await store.add(place.id, childKind(place.kind), quickParsed.names)
      showToast(made.length === 1 ? `Added ${made[0].name}` : `Added ${made.length} places`, 'success')
      quick = ''
    } catch (err) {
      showToast(failure(err), 'error')
    } finally {
      quickBusy = false
    }
  }

  let renameId = $state<string | null>(null)
  let renameText = $state('')
  let renameOpen = $state(false)
  const askRename = (id: string) => ((renameId = id), (renameText = store.byId.get(id)?.name ?? ''), (renameOpen = true))
  const rename = (id: string, name: string) => guard(() => store.rename(id, name))

  let moveId = $state<string | null>(null)
  let moveOpen = $state(false)
  const askMove = (id: string) => ((moveId = id), (moveOpen = true))
  const movePlace = (p: PlaceShort | null) => moveId && guard(() => store.move(moveId!, p?.id ?? null))

  let goesToOpen = $state(false)

  const reorder = (id: string, step: -1 | 1) => guard(() => store.reorder(id, step))
  const dropPlace = (id: string, target: string, edge: Edge) => {
    const pos = dropPosition(store.places, id, target, edge)
    return guard(() => store.move(id, pos.parent_id, pos.sort))
  }

  // Delete: a 409 says what is inside; the person picks where it goes, then it retries with move_to.
  let deleting = $state<{ place: TreePlace; counts: { places: number; items: number; lines: number } } | null>(null)
  let deleteTargetOpen = $state(false)
  let deleteOpen = $state(false)
  async function removePlace(p: TreePlace, moveTo?: string) {
    const parent = parentOf(p)
    try {
      await store.remove(p.id, moveTo)
      deleting = null
      deleteOpen = false
      if (selectedId === p.id || (selectedId && !store.byId.has(selectedId))) navigate(url(parent?.id ?? null), true)
      showToast(`Deleted ${p.name}`, 'info', 7000, {
        label: 'Undo',
        run: () => guard(async () => (await store.restore(p.id), select(p.id))),
      })
    } catch (e) {
      if (isNotEmpty(e)) ((deleting = { place: p, counts: notEmptyCounts(e.fields) }), (deleteOpen = true))
      else showToast(failure(e), 'error')
    }
  }

  // ---- items moving between places ----
  async function moveItems(items: Item[], target: PlaceShort | null) {
    const move = items.filter((i) => (i.place?.id ?? null) !== (target?.id ?? null))
    if (!move.length) return
    const was = new Map<string | null, string[]>()
    for (const i of move) was.set(i.place?.id ?? null, [...(was.get(i.place?.id ?? null) ?? []), i.id])
    const run = async (ids: string[], to: string | null) => {
      for (let i = 0; i < ids.length; i += 500) list.replace(...(await bulkItems(ids.slice(i, i + 500), { place_id: to })).data)
    }
    try {
      if (target) store.itemsMoved(move, target.id) // counts at once
      await run(move.map((i) => i.id), target?.id ?? null)
      if (!target) store.refresh()
      const word = move.length === 1 ? move[0].name : `${move.length} items`
      showToast(target ? `Moved ${word} to ${target.name}` : `${word} marked as not placed`, 'success', 7000, {
        label: 'Undo',
        run: () =>
          guard(async () => {
            for (const [from, ids] of was) await run(ids, from)
            store.refresh()
            if (selectedId) list.reset({ place_id: selectedId })
          }),
      })
    } catch (e) {
      store.refresh()
      showToast(failure(e), 'error')
    }
  }
  function dropItems(targetId: string) {
    const w = drag.what
    const t = store.byId.get(targetId)
    if (w?.kind === 'items' && t) moveItems(w.items, { id: t.id, name: t.name } as PlaceShort)
  }

  // ---- item detail ----
  const openItem = (id: string) => navigate(url(selectedId) + `#item=${encodeURIComponent(id)}`)
  const closeItem = () => {
    navigate(url(selectedId), true)
    store.refresh()
  }

  // ---- phone more menu ----
  let moreOpen = $state(false)
  let newHomeOpen = $state(false)
  let newHomeName = $state('New home')
  let copyRooms = $state(true)
  async function createHome() {
    const name = newHomeName.trim()
    if (!name) return
    newHomeOpen = false
    try {
      const h = await api.post<{ id: string }>('/api/v1/homes', { name, ...(copyRooms && store.homeId ? { copy_rooms_from: store.homeId } : {}) })
      inBoxesView = false
      await store.init(null)
      await store.switchHome(h.id)
      navigate('/places', true)
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  /** "In boxes" shows every box that is not unpacked instead of a home. */
  let inBoxesView = $state(false)
  async function archiveHome() {
    const h = store.home
    if (!h) return
    try {
      await api.patch(`/api/v1/homes/${h.id}`, { archived: true })
      showToast(`${h.name} archived`)
      await store.init(null)
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  const menu = 'flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-left text-[14px] hover:bg-chrome-hover'
  const subtitle = (p: TreePlace) => {
    const c = p.counts
    const kids = store.children(p.id).length
    return [kids ? `${kids} ${kids === 1 ? 'place' : 'places'}` : '', `${c?.items_below ?? 0} items`].filter(Boolean).join(' · ')
  }
</script>

{#snippet homeSwitcher()}
  <!-- Only when the household has more than one home. -->
  {#if store.homes.length > 1 || (store.homes.length === 1 && canWrite)}
    <Popover align="right">
      {#snippet trigger({ toggle, open })}
        <Button icon="home" onclick={toggle} aria-expanded={open}>{inBoxesView ? 'In boxes' : (store.home?.name ?? 'Home')}<Icon name="chevron-down" size={13} /></Button>
      {/snippet}
      {#each store.homes as h (h.id)}
        <button
          type="button"
          class="{menu} md:h-9"
          onclick={() => ((inBoxesView = false), store.switchHome(h.id), navigate('/places', true))}
        >
          <span class="min-w-0 flex-1 truncate">{h.name}</span>{#if h.id === store.homeId && !inBoxesView}<Icon name="check" class="text-accent" />{/if}
        </button>
      {/each}
      <button type="button" class="{menu} md:h-9" onclick={() => ((inBoxesView = true), navigate('/places', true))}>
        <Icon name="box" class="text-muted" /><span class="min-w-0 flex-1 truncate">In boxes</span>{#if inBoxesView}<Icon name="check" class="text-accent" />{/if}
      </button>
      {#if canWrite}
        <button type="button" class="{menu} md:h-9" onclick={() => ((newHomeName = 'New home'), (newHomeOpen = true))}><Icon name="plus" class="text-muted" />New home…</button>
      {/if}
      {#if can('admin') && !inBoxesView}
        <button type="button" class="{menu} md:h-9" onclick={archiveHome}><Icon name="archive" class="text-muted" />Archive {store.home?.name ?? 'home'}</button>
      {/if}
    </Popover>
  {/if}
{/snippet}

{#snippet headerActions()}
  {@render homeSwitcher()}
  {#if canWrite}<Button variant={docked.current ? 'secondary' : 'primary'} icon="plus" onclick={() => openAdd(null)}>Add {store.places.length ? 'room' : 'first room'}</Button>{/if}
{/snippet}

{#snippet noPlaces()}
  <EmptyState icon="pin" title="No places yet" message="Places say where things are: rooms, then furniture, then drawers and shelves. Start with a room.">
    {#if canWrite}<Button variant="primary" icon="plus" onclick={() => openAdd(null)}>Add a room</Button>{/if}
  </EmptyState>
{/snippet}

{#snippet error()}
  <EmptyState tone="error" title="Places could not load" message={store.error} onretry={() => store.load()} />
{/snippet}

<!-- The content of one place: header, quick add, spots, contents. -->
{#snippet placeBody(p: TreePlace, phone: boolean)}
  <PlaceHeader
    place={p}
    photo={photoOf(p)}
    {photoBusy}
    {canWrite}
    {phone}
    onphoto={(f) => setPhoto(p, f)}
    onadd={() => openAdd(p)}
    onrename={() => askRename(p.id)}
    onmove={() => askMove(p.id)}
    ongoesto={() => (goesToOpen = true)}
    ondelete={() => removePlace(p)}
  />
  {#if canWrite && !phone}
    <Toolbar>
      <form class="flex min-w-0 flex-1 items-center gap-2" onsubmit={quickAdd}>
        <label class="flex h-8 min-w-0 flex-[1_1_320px] items-center gap-2 rounded-control border border-border-input bg-surface px-2.5">
          <Icon name="plus" class="text-muted" />
          <span class="sr-only">Add inside {p.name}</span>
          <input bind:value={quick} placeholder="Add a {kindWord(childKind(p.kind))} here, or several: drawer 1–4" class="min-w-0 flex-1 border-0 bg-transparent text-[13px] outline-0" />
        </label>
        {#if quickParsed.names.length > 1}<span class="font-mono text-[12px] text-muted">{quickParsed.names.length} places</span>{/if}
        {#if quickParsed.error}<span class="text-[12px] text-danger">{quickParsed.error}</span>{/if}
        <Button type="submit" disabled={!quickParsed.names.length || quickBusy}>Add</Button>
      </form>
    </Toolbar>
  {/if}
  {#if spots.length}
    <SectionBar icon="drawer" title="Inside {p.name}"><span class="font-mono text-muted">{spots.length}</span></SectionBar>
    <SpotsStrip {spots} {canWrite} {phone} onopen={select} onadd={() => openAdd(p)} ondropitems={dropItems} />
  {:else if canWrite}
    <div class="flex flex-col items-center gap-2 border-b border-divider bg-surface-subtle px-6 py-6 text-center">
      <p class="m-0 max-w-[48ch] text-[13px] text-muted">
        {#if store.onlyRooms}
          This is one of your starter rooms. Add its furniture ("Wardrobe", "Desk") and then the spots in it ("drawer 1–4"); things can sit in any of them.
        {:else}
          Nothing is inside {p.name}. Add {kindWord(childKind(p.kind), 2)} if it has parts, such as "drawer 1–4".
        {/if}
      </p>
      <Button icon="plus" onclick={() => openAdd(p)}>Add {kindWord(childKind(p.kind), 2)}</Button>
    </div>
  {/if}
  <PlaceContents
    place={p}
    {list}
    {catalog}
    {canWrite}
    {phone}
    {openItemId}
    highlightLine={lineParam}
    onopenitem={openItem}
    onmoveitems={moveItems}
    onchanged={() => store.refresh()}
  />
{/snippet}

{#if docked.current}
  <Panel>
    <PageHeader icon="pin" title="Places" breadcrumb={[activeHousehold()?.name ?? 'Household', ...(store.homes.length > 1 && store.home ? [store.home.name] : [])]}>
      {#snippet actions()}{@render headerActions()}{/snippet}
    </PageHeader>
    {#if inBoxesView}
      <InBoxes />
    {:else if store.error && !store.places.length}
      {@render error()}
    {:else if store.loaded && !store.places.length}
      {@render noPlaces()}
    {:else}
      <div class="flex min-h-0 flex-1">
        <nav aria-label="Places tree" class="w-[280px] flex-none overflow-y-auto border-r border-divider bg-surface-subtle">
          {#if !store.loaded}<Skeleton rows={6} label="Loading places" />{/if}
          <PlaceTree {store} {selectedId} {canWrite} onselect={select} onrename={rename} onreorder={reorder} onmove={askMove} ondropplace={dropPlace} ondropitems={dropItems} />
        </nav>
        <section aria-label="Place" class="flex min-w-0 flex-1 flex-col overflow-y-auto">
          {#if place}
            {@render placeBody(place, false)}
          {:else}
            <div class="flex flex-1 items-center justify-center p-8 text-[14px] text-muted">{store.loaded ? 'Pick a place to see what is in it.' : ''}</div>
          {/if}
        </section>
        {#if openItemId && catalog}
          <aside aria-label="Item detail" class="flex w-[380px] flex-none flex-col border-l border-divider">
            <div class="flex h-11 flex-none items-center gap-2 border-b border-divider bg-surface-subtle px-4">
              <Button variant="ghost" class="h-8" icon="chevron-left" onclick={closeItem}>Back to {place?.name ?? 'place'}</Button>
            </div>
            <ItemDetail id={openItemId} {list} {catalog} onremoved={closeItem} />
          </aside>
        {/if}
      </div>
      <PanelFooter>
        <span>{store.places.length} {store.places.length === 1 ? 'place' : 'places'}</span>
        <span class="ml-auto text-[11px]">Arrow keys move in the tree · Alt+↑↓ reorder · M move · F2 rename · drag to move</span>
      </PanelFooter>
    {/if}
  </Panel>
{:else if place && !inBoxesView}
  <!-- Phone: a place is its own screen. -->
  <FlowFrame icon="pin" title={place.name} breadcrumb={['Places']}>
    {@render placeBody(place, true)}
    {#snippet bar()}
      {#if canWrite}
        <Button variant="primary" icon="plus" class="h-14 flex-[2] rounded-[14px] text-[15px]" onclick={() => openAdd(place)}>Add spots</Button>
        <Popover bind:open={moreOpen} align="right" up>
          {#snippet trigger({ toggle, open })}
            <Button icon="more" class="h-14 flex-1 rounded-[14px]" onclick={toggle} aria-expanded={open}>More</Button>
          {/snippet}
          <button type="button" class={menu} onclick={() => ((moreOpen = false), askRename(place!.id))}><Icon name="edit" class="text-muted" />Rename</button>
          <button type="button" class={menu} onclick={() => ((moreOpen = false), askMove(place!.id))}><Icon name="move" class="text-muted" />Move to…</button>
          <button type="button" class={menu} onclick={() => ((moreOpen = false), (goesToOpen = true))}><Icon name="truck" class="text-muted" />Where it goes…</button>
          <button type="button" class="{menu} text-danger" onclick={() => ((moreOpen = false), removePlace(place!))}><Icon name="trash" />Delete</button>
        </Popover>
      {/if}
    {/snippet}
  </FlowFrame>
  {#if openItemId && catalog}
    <ItemDetail id={openItemId} {list} {catalog} phone onremoved={closeItem} />
  {/if}
{:else}
  <Panel>
    <PageHeader icon="pin" title="Places" breadcrumb={[activeHousehold()?.name ?? 'Household']}>
      {#snippet actions()}{@render headerActions()}{/snippet}
    </PageHeader>
    {#if inBoxesView}
      <InBoxes />
    {:else if store.error && !store.places.length}
      {@render error()}
    {:else if store.loaded && !store.places.length}
      {@render noPlaces()}
    {:else}
      <div class="min-h-0 flex-1 overflow-y-auto bg-surface-sunken pb-6">
        {#each store.roots as root (root.id)}
          {@const kids = store.children(root.id)}
          <Group title={root.name}>
            <button type="button" onclick={() => select(root.id)} class="flex min-h-14 w-full cursor-pointer items-center gap-3 px-4 text-left">
              <Icon name={kindIcon[root.kind] ?? 'pin'} class="text-label" />
              <span class="min-w-0 flex-1"><b class="block truncate text-[15px] font-semibold">Everything in {root.name}</b><span class="font-mono text-[12px] text-muted">{subtitle(root)}</span></span>
              <Icon name="chevron-right" class="text-faint" />
            </button>
            {#each kids as k (k.id)}
              <button type="button" onclick={() => select(k.id)} class="flex min-h-14 w-full cursor-pointer items-center gap-3 px-4 pl-8 text-left">
                <Icon name={kindIcon[k.kind] ?? 'pin'} class="text-label" />
                <span class="min-w-0 flex-1"><b class="block truncate text-[15px] font-semibold">{k.name}</b><span class="font-mono text-[12px] text-muted">{subtitle(k)}</span></span>
                <Icon name="chevron-right" class="text-faint" />
              </button>
            {/each}
          </Group>
        {/each}
        {#if store.onlyRooms && canWrite}
          <p class="m-0 px-4 pt-4 text-[13px] text-muted">These are your starter rooms. Open one to add furniture, then spots like "drawer 1–4".</p>
        {/if}
      </div>
    {/if}
  </Panel>
{/if}

<AddPlacesDialog bind:open={addOpen} parent={addParent} onadd={addPlaces} />

<Dialog bind:open={newHomeOpen} title="New home">
  <form id="new-home-form" class="flex flex-col gap-3" onsubmit={(e) => { e.preventDefault(); createHome() }}>
    <Field label="Name" bind:value={newHomeName} autocomplete="off" />
    {#if store.home}
      <label class="flex items-center gap-2 text-[14px]"><input type="checkbox" bind:checked={copyRooms} class="size-[18px]" />Copy the room names from {store.home.name}</label>
    {/if}
  </form>
  {#snippet actions()}
    <Button onclick={() => (newHomeOpen = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="new-home-form">Create home</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={renameOpen} title="Rename">
  <form id="rename-form" onsubmit={(e) => { e.preventDefault(); const id = renameId; renameOpen = false; if (id && renameText.trim()) rename(id, renameText.trim()) }}>
    <Field label="Name" bind:value={renameText} autocomplete="off" />
  </form>
  {#snippet actions()}
    <Button onclick={() => (renameOpen = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="rename-form">Save</Button>
  {/snippet}
</Dialog>

<PlaceDialog bind:open={moveOpen} title="Move {moveId ? (store.byId.get(moveId)?.name ?? 'place') : 'place'} to…" allowNone onpick={movePlace} />
<PlaceDialog bind:open={goesToOpen} title="Where does {place?.name ?? 'it'} go in the new home?" value={null} allowNone onpick={(p) => place && guard(() => store.setGoesTo(place.id, p?.id ?? null))} />

<Dialog bind:open={deleteOpen} title="Delete {deleting?.place.name ?? ''}?">
  {#if deleting}
    <p class="m-0 text-[14px] text-body">
      It still holds {deleting.counts.places} {deleting.counts.places === 1 ? 'place' : 'places'}, {deleting.counts.items} {deleting.counts.items === 1 ? 'item' : 'items'} and
      {deleting.counts.lines} contents {deleting.counts.lines === 1 ? 'line' : 'lines'}. Choose where they should go.
    </p>
  {/if}
  {#snippet actions()}
    <Button onclick={() => (deleteOpen = false)}>Keep it</Button>
    <Button variant="primary" icon="move" onclick={() => (deleteTargetOpen = true)}>Move them to…</Button>
  {/snippet}
</Dialog>
<PlaceDialog bind:open={deleteTargetOpen} title="Move the contents to" onpick={(p) => deleting && p && removePlace(deleting.place, p.id)} />

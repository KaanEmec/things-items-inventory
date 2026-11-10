<script lang="ts">
  import { auth } from '../auth.svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'
  import Select from '../ui/Select.svelte'
  import { ancestorIds, buildRows, getTree, listHomes, recentPlaces, rememberPlace, searchPlaces } from './places'
  import type { Home, PlaceShort, TreePlace } from './types'

  interface Props {
    /** The current place, marked in the tree. */
    value?: PlaceShort | null
    onpick: (place: PlaceShort | null) => void
    /** Offer "Not placed" (a null place). */
    allowNone?: boolean
  }
  let { value = null, onpick, allowNone = false }: Props = $props()

  const kindIcon: Record<string, IconName> = { floor: 'stairs', room: 'door', furniture: 'cabinet', spot: 'drawer', box: 'box' }

  let homes = $state<Home[]>([])
  /* svelte-ignore state_referenced_locally */
  let homeId = $state(value?.home_id ?? '')
  let places = $state.raw<TreePlace[]>([])
  let expanded = $state.raw(new Set<string>())
  let q = $state('')
  let loading = $state(true)
  let error = $state('')
  const recents = recentPlaces(auth.householdId)

  $effect(() => {
    listHomes().then(
      (p) => {
        homes = p.data
        if (!homeId || !homes.some((h) => h.id === homeId)) homeId = homes[0]?.id ?? ''
        if (!homeId) loading = false
      },
      (e) => {
        error = explain(e).message
        loading = false
      },
    )
  })

  $effect(() => {
    const id = homeId
    if (!id) return
    loading = true
    getTree(id).then(
      (t) => {
        if (id !== homeId) return
        places = t.places
        const open = ancestorIds(t.places, value?.id)
        for (const p of t.places) if (!p.parent_id) open.add(p.id)
        expanded = open
        loading = false
      },
      (e) => {
        error = explain(e).message
        loading = false
      },
    )
  })

  const rows = $derived(buildRows(places))
  const visible = $derived.by(() => {
    const out = []
    let skip = -1
    for (const r of rows) {
      if (skip >= 0 && r.depth > skip) continue
      skip = -1
      out.push(r)
      if (r.hasChildren && !expanded.has(r.place.id)) skip = r.depth
    }
    return out
  })
  const found = $derived(searchPlaces(places, q))

  function toggle(id: string) {
    const next = new Set(expanded)
    if (!next.delete(id)) next.add(id)
    expanded = next
  }
  function pick(p: PlaceShort) {
    rememberPlace(auth.householdId, p)
    onpick(p)
  }
  const rowCls = 'flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2 text-left text-[14px] hover:bg-chrome-hover md:h-9'
</script>

<div class="flex flex-col gap-3">
  <label class="flex h-11 items-center gap-2 rounded-control border border-border-input bg-surface px-3 md:h-9">
    <Icon name="search" class="text-muted" />
    <span class="sr-only">Search places</span>
    <input bind:value={q} placeholder="Search places" class="min-w-0 flex-1 border-0 bg-transparent text-[16px] outline-0 md:text-[14px]" />
  </label>

  {#if error}<p role="alert" class="m-0 text-[13px] text-danger">{error}</p>{/if}

  {#if allowNone && !q}
    <button type="button" class={rowCls} onclick={() => onpick(null)}>
      <Icon name="x" class="text-muted" />Not placed
      {#if !value}<Icon name="check" class="ml-auto text-accent" />{/if}
    </button>
  {/if}

  {#if !q && recents.length}
    <div>
      <div class="px-2 pb-1 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">Recent</div>
      {#each recents as p (p.id)}
        <button type="button" class={rowCls} onclick={() => pick(p)}>
          <Icon name={kindIcon[p.kind] ?? 'pin'} class="text-label" />
          <span class="min-w-0 flex-1 truncate">{p.name}<span class="text-muted"> · {p.path.split(' · ').slice(1).join(' · ')}</span></span>
        </button>
      {/each}
    </div>
  {/if}

  {#if homes.length > 1 && !q}
    <Select label="Home" bind:value={homeId} options={homes.map((h) => ({ value: h.id, label: h.name }))} />
  {/if}

  <div class="max-h-[46dvh] overflow-y-auto" role="tree" aria-label="Places">
    {#if loading}
      <p class="m-0 px-2 py-3 text-[14px] text-muted">Loading places</p>
    {:else if q}
      {#each found as p (p.id)}
        <button type="button" role="treeitem" aria-selected={p.id === value?.id} class={rowCls} onclick={() => pick(p)}>
          <Icon name={kindIcon[p.kind] ?? 'pin'} class="text-label" />
          <span class="min-w-0 flex-1 truncate">{p.name}<span class="text-muted"> · {p.path.split(' · ').slice(1).join(' · ')}</span></span>
        </button>
      {:else}
        <p class="m-0 px-2 py-3 text-[14px] text-muted">No place matches "{q}".</p>
      {/each}
    {:else if !places.length}
      <p class="m-0 px-2 py-3 text-[14px] text-muted">This home has no places yet. Add rooms and furniture under Places first.</p>
    {:else}
      {#each visible as r (r.place.id)}
        {@const p = r.place}
        <div class="flex items-center" style:padding-left="{r.depth * 16}px" role="treeitem" aria-selected={p.id === value?.id} aria-expanded={r.hasChildren ? expanded.has(p.id) : undefined}>
          {#if r.hasChildren}
            <button type="button" onclick={() => toggle(p.id)} aria-label="{expanded.has(p.id) ? 'Collapse' : 'Expand'} {p.name}" class="flex size-11 flex-none cursor-pointer items-center justify-center rounded-control text-muted hover:bg-chrome-hover md:size-9">
              <Icon name={expanded.has(p.id) ? 'chevron-down' : 'chevron-right'} />
            </button>
          {:else}
            <span class="size-11 flex-none md:size-9"></span>
          {/if}
          <button type="button" class="{rowCls} {p.id === value?.id ? 'bg-accent-row font-semibold' : ''}" onclick={() => pick(p)}>
            <Icon name={kindIcon[p.kind] ?? 'pin'} class="text-label" />
            <span class="min-w-0 flex-1 truncate">{p.name}</span>
            {#if p.counts}<span class="font-mono text-[12px] text-muted">{p.counts.items_below}</span>{/if}
            {#if p.id === value?.id}<Icon name="check" class="text-accent" />{/if}
          </button>
        </div>
      {/each}
    {/if}
  </div>
</div>

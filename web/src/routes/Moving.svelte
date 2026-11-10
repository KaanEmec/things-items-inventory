<script lang="ts">
  import { onMount } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'
  import { activeHousehold, can } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import Icon from '../lib/Icon.svelte'
  import BoxDetail from '../lib/moving/BoxDetail.svelte'
  import BoxLabel from '../lib/moving/BoxLabel.svelte'
  import { bulkBoxes, createBox, listBoxes, listRooms, type Box, type Handling, type Room } from '../lib/moving/boxes'
  import { countBoxes, filterBoxes, parseBoxNumber, roomChips, STATUS_LABEL } from '../lib/moving/logic'
  import { packing } from '../lib/moving/packing.svelte'
  import PrintPanel from '../lib/moving/PrintPanel.svelte'
  import { HANDLING_LABEL, roomTint } from '../lib/moving/print'
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import Toolbar from '../lib/shell/Toolbar.svelte'
  import { stream } from '../lib/stream.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import Dialog from '../lib/ui/Dialog.svelte'
  import Select from '../lib/ui/Select.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  const docked = new MediaQuery('min-width: 768px')
  const canWrite = $derived(can('member'))
  const household = $derived(activeHousehold()?.name ?? '')

  let boxes = $state.raw<Box[]>([])
  let rooms = $state.raw<Room[]>([])
  let loaded = $state(false)
  let error = $state('')
  let room = $state('')
  let typed = $state('')
  let selecting = $state(false)
  let selected = $state.raw<string[]>([])
  let detailId = $state<string | null>(null)
  let detailOpen = $state(false)
  let printOpen = $state(false)
  let createFor = $state<number | null>(null)
  let bulkRoom = $state('')

  const counts = $derived(countBoxes(boxes))
  const chips = $derived(roomChips(boxes))
  const shown = $derived(filterBoxes(boxes, room))
  const hasNoRoom = $derived(boxes.some((b) => !b.goes_to))

  async function refresh() {
    try {
      boxes = await listBoxes()
      error = ''
    } catch (e) {
      error = explain(e).message
    }
    loaded = true
  }
  onMount(() => {
    refresh()
    packing.load().catch(() => {})
    listRooms().then((r) => (rooms = r), () => {})
  })
  $effect(() => stream.onBurst(['box', 'item', 'resync'], refresh))

  /** Type a number, press Enter: that box becomes the "packing now" box. */
  async function submit(e: SubmitEvent) {
    e.preventDefault()
    const n = parseBoxNumber(typed)
    if (n === null) return showToast('Type a box number, like 14.', 'error')
    const known = boxes.find((b) => b.box_number === n)
    if (!known) {
      createFor = n
      return
    }
    await startPacking(known)
  }
  async function startPacking(b: Box) {
    try {
      await packing.set({ box_id: b.id })
      typed = ''
      await refresh()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  async function confirmCreate() {
    const n = createFor
    createFor = null
    if (n === null) return
    try {
      const made = (await createBox(n)).data[0]
      await refresh()
      if (made) await startPacking(made)
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  async function closeBox() {
    const b = packing.box
    try {
      // A box that was unpacked or is already on its way cannot be closed: stop packing it.
      const now = b && boxes.find((x) => x.id === b.id)?.box_status
      if (b && (now === 'unpacked' || now === 'arrived')) {
        await packing.set(null)
        showToast(`Box ${b.box_number} is already ${now}. Type another number.`)
        return
      }
      await packing.close()
      showToast(`Box ${b?.box_number} packed. Type the next number.`)
      await refresh()
    } catch (e) {
      showToast(explain(e).message, 'error')
      await packing.load().catch(() => {})
    }
  }
  async function newBox() {
    try {
      const made = (await createBox()).data[0]
      await refresh()
      if (made) await startPacking(made)
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  function open(b: Box) {
    if (selecting) selected = selected.includes(b.id) ? selected.filter((x) => x !== b.id) : [...selected, b.id]
    else {
      detailId = b.id
      detailOpen = true
    }
  }
  async function applyBulk(body: { goes_to_id?: string | null; handling?: Handling[] }) {
    try {
      await bulkBoxes({ ids: selected, ...body })
      await refresh()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  const status = (b: Box) => (packing.box?.id === b.id ? 'Packing now' : STATUS_LABEL[b.box_status ?? 'empty'])
</script>

<Panel>
  <PageHeader icon="truck" title="Moving" breadcrumb={['Old home', 'New home']}>
    {#snippet actions()}
      {#if !docked.current}<Button icon="printer" aria-label="Print labels" onclick={() => (printOpen = true)} />{/if}
      {#if canWrite}<Button variant="primary" icon="plus" onclick={newBox}>New box</Button>{/if}
    {/snippet}
  </PageHeader>

  <!-- Counts strip -->
  <div class="grid flex-none grid-cols-2 border-b border-divider md:grid-cols-[repeat(4,1fr)_2fr]" aria-label="Counts">
    {#each [['packed', 'Packed'], ['open', 'Open'], ['empty', 'Empty'], ['unpacked', 'Unpacked']] as [k, label] (k)}
      <div class="border-r border-divider px-4 py-3 md:px-5">
        <b class="block text-[22px] leading-tight font-bold tabular-nums">{counts.byStatus[k as keyof typeof counts.byStatus]}</b>
        <span class="text-[12px] font-medium text-muted">{label}</span>
      </div>
    {/each}
    <div class="col-span-2 flex flex-col justify-center gap-1.5 px-4 py-3 md:col-span-1 md:px-5">
      <span class="flex justify-between text-[12px] font-medium text-muted"><span>{counts.items} items in boxes</span><span class="tabular-nums">{counts.percent}%</span></span>
      <span class="h-2 overflow-hidden rounded-full bg-surface-sunken"><span class="block h-full bg-accent" style:width="{counts.percent}%"></span></span>
    </div>
  </div>

  <Toolbar>
    <form onsubmit={submit} class="flex h-11 items-center gap-2 rounded-control border-2 border-ink bg-surface pr-1 pl-2.5 md:h-9">
      <label for="box-no" class="text-[11px] font-extrabold tracking-[0.08em]">BOX #</label>
      <input id="box-no" bind:value={typed} inputmode="numeric" placeholder="Type a number" disabled={!canWrite} class="w-28 border-0 bg-transparent text-[18px] font-extrabold outline-none" />
    </form>
    <span class="mx-1 hidden h-5 w-px bg-border md:block"></span>
    <div class="flex min-w-0 flex-1 gap-1.5 overflow-x-auto" role="group" aria-label="Filter by room">
      {#each [{ id: '', name: 'All', color: null, count: boxes.length }, ...chips, ...(hasNoRoom && chips.length ? [{ id: 'none', name: 'No room yet', color: null, count: boxes.filter((b) => !b.goes_to).length }] : [])] as c (c.id)}
        <button
          type="button"
          aria-pressed={room === c.id}
          onclick={() => (room = c.id)}
          class="inline-flex h-9 flex-none cursor-pointer items-center gap-1.5 rounded-control border px-2.5 text-[13px] font-medium {room === c.id ? 'border-ink bg-ink text-white' : 'border-border-input bg-surface text-body'}"
        >
          {#if c.color}<span class="size-[11px] rounded-[3px] border-[1.5px] border-ink" style:background={roomTint(c.color).bg}></span>{/if}
          {c.name} <span class="text-[11px] opacity-75">{c.count}</span>
        </button>
      {/each}
    </div>
    {#if canWrite}
      <Button variant={selecting ? 'primary' : 'secondary'} icon="check" aria-pressed={selecting} onclick={() => ((selecting = !selecting), (selected = []))}>Select</Button>
    {/if}
  </Toolbar>

  {#if selecting && selected.length}
    <Toolbar>
      <span class="text-[13px] font-semibold">{selected.length} selected</span>
      <div class="w-48"><Select label="Goes to" bind:value={bulkRoom} onchange={() => bulkRoom && applyBulk({ goes_to_id: bulkRoom })} options={[{ value: '', label: 'Set room…' }, ...rooms.map((r) => ({ value: r.id, label: r.name }))]} /></div>
      {#each Object.entries(HANDLING_LABEL) as [h, label] (h)}
        <Button onclick={() => applyBulk({ handling: [h as Handling] })}>{label}</Button>
      {/each}
      <Button variant="ghost" onclick={() => applyBulk({ handling: [] })}>Clear marks</Button>
    </Toolbar>
  {/if}

  <div class="flex min-h-0 flex-1 flex-wrap overflow-auto">
    <section aria-label="Boxes" class="min-w-0 flex-[999_1_360px] border-divider bg-surface-sunken md:border-r" style="background-image: radial-gradient(var(--color-border) 1px, transparent 1px); background-size: 16px 16px">
      <div class="p-4 md:p-6">
        {#if error}
          <EmptyState tone="error" compact title="Boxes could not load" message={error} onretry={refresh} />
        {:else if loaded && !boxes.length}
          <EmptyState compact icon="box" title="No boxes yet" message="Use New box, or type a number above." />
        {/if}
        <ul class="m-0 grid list-none grid-cols-1 gap-4 p-0 min-[480px]:grid-cols-2 md:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]">
          {#each shown as b (b.id)}
            {@const now = packing.box?.id === b.id}
            <li>
              <button
                type="button"
                onclick={() => open(b)}
                aria-pressed={selecting ? selected.includes(b.id) : undefined}
                class="block w-full cursor-pointer rounded-[10px] border-0 bg-transparent p-0 text-left {now || selected.includes(b.id) ? 'outline-[3px] outline-offset-3 outline-accent' : ''}"
              >
                <BoxLabel number={b.box_number} room={b.goes_to} handling={b.handling} />
                <span class="mt-1.5 flex items-center gap-1.5 text-[12px] font-semibold {now ? 'text-accent' : 'text-body'}">
                  {#if now}<Icon name="box" size={13} />{/if}{status(b)}
                  <span class="ml-auto font-medium text-muted tabular-nums">{b.counts?.items_below ?? 0}</span>
                </span>
              </button>
            </li>
          {/each}
        </ul>
      </div>
    </section>
    {#if docked.current}
      <div class="flex min-w-[260px] flex-[1_1_300px] flex-col">
        <div class="flex h-11 flex-none items-center gap-2 border-b border-divider bg-surface-subtle px-5 text-[13px] font-semibold"><Icon name="printer" size={16} class="text-muted" />Print labels</div>
        <PrintPanel {boxes} {selected} />
      </div>
    {/if}
  </div>

  <!-- Packing bar: Packing box N, Close box. Phone: bottom bar of the screen. -->
  {#if packing.box}
    <div class="flex flex-none items-center gap-3 border-t border-divider bg-surface-subtle px-4 py-2 md:px-6" role="status">
      <Icon name="box" size={18} class="text-accent" />
      <span class="min-w-0 flex-1 truncate text-[14px] font-bold">Packing box {packing.box.box_number}{packing.box.goes_to ? ` · ${packing.box.goes_to.name}` : ''}</span>
      {#if canWrite}<Button variant="primary" onclick={closeBox}>Close box</Button>{/if}
    </div>
  {/if}
</Panel>

<BoxDetail bind:open={detailOpen} boxId={detailId} {rooms} {household} onchange={refresh} />

<Dialog open={createFor !== null} title="New box" >
  <p class="m-0 text-[14px]">Box {createFor} does not exist yet. Create it and start packing it?</p>
  {#snippet actions()}
    <Button onclick={() => (createFor = null)}>Cancel</Button>
    <Button variant="primary" onclick={confirmCreate}>Create box {createFor}</Button>
  {/snippet}
</Dialog>

{#if !docked.current}
  <Dialog bind:open={printOpen} title="Print labels" size="lg"><PrintPanel {boxes} {selected} /></Dialog>
{/if}

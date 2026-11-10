<script lang="ts">
  import { explain } from '../errors'
  import { api, type Page } from '../api'
  import { auth, can } from '../auth.svelte'
  import { ago } from '../format'
  import Icon from '../Icon.svelte'
  import { sentence, type HistoryEntry } from '../inventory/history'
  import PlacePicker from '../inventory/PlacePicker.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import Select from '../ui/Select.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import Unpack from './Unpack.svelte'
  import BoxLabel from './BoxLabel.svelte'
  import { getBoxDetail, patchBox, unpackBox, type BoxDetail, type Handling, type Room } from './boxes'
  import { STATUS_LABEL } from './logic'
  import { packing } from './packing.svelte'
  import { HANDLING_LABEL } from './print'

  interface Props {
    open?: boolean
    boxId: string | null
    rooms: Room[]
    household?: string
    /** Called after anything changed, so the grid reloads. */
    onchange?: () => void
  }
  let { open = $bindable(false), boxId, rooms, household = '', onchange }: Props = $props()

  let box = $state.raw<BoxDetail | null>(null)
  let history = $state.raw<HistoryEntry[]>([])
  let error = $state('')
  let picking = $state(false)
  let unpackOpen = $state(false)
  const canWrite = $derived(can('member'))
  const marks = Object.keys(HANDLING_LABEL) as Handling[]

  $effect(() => {
    if (open && boxId) load(boxId)
    else picking = false
  })

  async function load(id: string) {
    error = ''
    try {
      box = await getBoxDetail(id)
      history = (await api.get<Page<HistoryEntry>>(`/api/v1/places/${encodeURIComponent(id)}/history?limit=8`)).data
    } catch (e) {
      error = explain(e).message
    }
  }
  async function save(body: Parameters<typeof patchBox>[1]) {
    if (!box) return
    try {
      await patchBox(box.id, body)
      await load(box.id)
      onchange?.()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  const toggle = (h: Handling) => {
    const cur = box?.handling ?? []
    save({ handling: cur.includes(h) ? cur.filter((x) => x !== h) : [...cur, h] })
  }
  async function moveOut(place: { id: string } | null) {
    picking = false
    if (!box || !place) return
    try {
      await unpackBox(box.id, {
        moves: [...box.items.map((i) => ({ item_id: i.id, place_id: place.id })), ...box.lines.map((l) => ({ line_id: l.id, place_id: place.id }))],
      })
      await load(box.id)
      onchange?.()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  async function packHere() {
    if (!box) return
    try {
      await packing.set({ box_id: box.id })
      showToast(`Packing box ${box.box_number}`)
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  const empty = $derived(!box || (box.items.length === 0 && box.lines.length === 0))
</script>

<Dialog bind:open title={box ? `Box ${box.box_number}` : 'Box'} size="lg">
  {#if error}
    <p class="m-0 text-[13px] text-danger" role="alert">{error}</p>
  {:else if box}
    <div class="flex flex-col gap-4">
      <BoxLabel number={box.box_number} room={box.goes_to} handling={box.handling} {household} />
      <p class="m-0 text-[13px] text-muted">{STATUS_LABEL[box.box_status ?? 'empty']}</p>
      {#if canWrite}
        <Select
          label="Goes to"
          value={box.goes_to?.id ?? ''}
          onchange={(e) => save({ goes_to_id: e.currentTarget.value || null })}
          options={[{ value: '', label: 'No room yet' }, ...rooms.map((r) => ({ value: r.id, label: `${r.name} · ${r.home}` }))]}
        />
        <div role="group" aria-label="Handling marks" class="flex flex-wrap gap-2">
          {#each marks as h (h)}
            <button
              type="button"
              aria-pressed={box.handling.includes(h)}
              onclick={() => toggle(h)}
              class="h-9 cursor-pointer rounded-control border px-3 text-[13px] font-medium {box.handling.includes(h) ? 'border-ink bg-ink text-white' : 'border-border-input bg-surface text-body'}"
            >{HANDLING_LABEL[h]}</button>
          {/each}
        </div>
      {/if}

      <section aria-label="Contents">
        <h3 class="m-0 mb-1 text-[13px] font-semibold">Contents</h3>
        {#if empty}
          <p class="m-0 text-[13px] text-muted">Nothing in this box yet.</p>
        {:else}
          <ul class="m-0 list-none p-0 text-[14px]">
            {#each box.items as i (i.id)}<li class="py-0.5"><a href="/items#item={i.id}">{i.name}</a></li>{/each}
            {#each box.lines as l (l.id)}<li class="py-0.5">{l.name}{l.quantity > 1 ? ` ×${l.quantity}` : ''}</li>{/each}
          </ul>
        {/if}
      </section>

      <section aria-label="History">
        <h3 class="m-0 mb-1 text-[13px] font-semibold">History</h3>
        {#each history as e (e.id)}
          <p class="m-0 py-0.5 text-[13px] text-body">{sentence(e, auth.user?.id)} <span class="text-muted">{ago(e.at)}</span></p>
        {:else}
          <p class="m-0 text-[13px] text-muted">No history yet.</p>
        {/each}
      </section>

      {#if picking}
        <PlacePicker onpick={moveOut} />
      {/if}
    </div>
  {/if}
  {#snippet actions()}
    {#if canWrite && box}
      {#if box.box_status === 'packed'}<Button icon="check" onclick={() => save({ box_status: 'arrived' })}>Arrived</Button>{/if}
      {#if box.box_status === 'packed' || box.box_status === 'arrived'}<Button variant="primary" icon="box" onclick={() => (unpackOpen = true)}>Unpack</Button>{/if}
      <Button icon="move" disabled={empty} onclick={() => (picking = !picking)}>Move items out</Button>
      {#if box.box_status === 'empty' || box.box_status === 'open'}
        <Button variant="primary" icon="box" onclick={packHere}>Pack this box</Button>
      {/if}
    {/if}
  {/snippet}
</Dialog>

<Unpack bind:open={unpackOpen} {box} onchange={() => (boxId && load(boxId), onchange?.())} />

<script lang="ts">
  import { auth } from '../auth.svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import { recentPlaces, rememberPlace } from '../inventory/places'
  import PlacePicker from '../inventory/PlacePicker.svelte'
  import type { PlaceShort } from '../inventory/types'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { unpackBox, type BoxDetail } from './boxes'
  import { fillRest, toMoves, undecided, type Choices, type Thing } from './unpack'

  interface Props {
    open?: boolean
    box: BoxDetail | null
    onchange?: () => void
  }
  let { open = $bindable(false), box, onchange }: Props = $props()

  let choices = $state.raw<Choices>({})
  let names = $state.raw<Record<string, string>>({})
  let picking = $state<string | null>(null)
  let busy = $state(false)

  const things = $derived<Thing[]>(box ? [...box.items.map((i) => ({ id: i.id, kind: 'item' as const })), ...box.lines.map((l) => ({ id: l.id, kind: 'line' as const }))] : [])
  const label = (t: Thing) => (t.kind === 'item' ? box?.items.find((i) => i.id === t.id)?.name : box?.lines.find((l) => l.id === t.id)?.name) ?? ''
  const room = $derived(box?.goes_to ?? null)
  const recents = $derived(recentPlaces(auth.householdId).slice(0, 4))
  const left = $derived(undecided(things, choices))

  $effect(() => {
    if (open) {
      choices = {}
      names = {}
      picking = null
    }
  })

  function choose(t: Thing, p: { id: string; name: string } | null) {
    choices = { ...choices, [t.id]: { place: p?.id ?? null } }
    names = { ...names, [t.id]: p ? p.name : 'Not found' }
    picking = null
  }
  function everything() {
    if (!room) return
    choices = fillRest(things, choices, room.id)
    const n = { ...names }
    for (const t of things) if (!n[t.id]) n[t.id] = room.name
    names = n
  }
  async function done() {
    if (!box) return
    busy = true
    try {
      const b = await unpackBox(box.id, { moves: toMoves(things, choices) })
      showToast(b.box_status === 'unpacked' ? `Box ${box.box_number} unpacked` : `${left} left in box ${box.box_number}`)
      onchange?.()
      open = false
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
    busy = false
  }
  const chip = 'h-9 cursor-pointer rounded-control border border-border-input bg-surface px-2.5 text-[13px] font-medium text-body'
</script>

<Dialog bind:open title={box ? `Unpack box ${box.box_number}` : 'Unpack'} size="lg">
  {#if box}
    {#if room}
      <Button class="mb-3" icon="door" onclick={everything} disabled={!left}>Put everything else in {room.name}</Button>
    {/if}
    <ul class="m-0 flex list-none flex-col divide-y divide-divider-row p-0">
      {#each things as t (t.id)}
        <li class="flex flex-col gap-2 py-3">
          <div class="flex items-center gap-2 text-[14px] font-medium">
            {#if choices[t.id]}<Icon name="check" size={15} class="text-accent" />{/if}
            <span class="min-w-0 flex-1 truncate">{label(t)}</span>
            {#if choices[t.id]}<span class="text-[12px] text-muted">{names[t.id]}</span>{/if}
          </div>
          <div class="flex flex-wrap gap-1.5">
            {#if room}<button type="button" class={chip} onclick={() => choose(t, room)}>{room.name}</button>{/if}
            {#each recents as p (p.id)}<button type="button" class={chip} onclick={() => (rememberPlace(auth.householdId, p), choose(t, p))}>{p.name}</button>{/each}
            <button type="button" class={chip} onclick={() => (picking = picking === t.id ? null : t.id)}>Choose place…</button>
            <button type="button" class={chip} onclick={() => choose(t, null)}>Not found</button>
          </div>
          {#if picking === t.id}
            <PlacePicker onpick={(p: PlaceShort | null) => (p && rememberPlace(auth.householdId, p), choose(t, p))} />
          {/if}
        </li>
      {:else}
        <li class="py-3 text-[14px] text-muted">The box is empty.</li>
      {/each}
    </ul>
  {/if}
  {#snippet actions()}
    <Button onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" disabled={busy || left === things.length} onclick={done}>{left ? `Done (${left} stay in the box)` : 'Done'}</Button>
  {/snippet}
</Dialog>

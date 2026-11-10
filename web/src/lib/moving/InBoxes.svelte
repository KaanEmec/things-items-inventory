<script lang="ts">
  import { onMount } from 'svelte'
  import { can } from '../auth.svelte'
  import { explain } from '../errors'
  import { stream } from '../stream.svelte'
  import Button from '../ui/Button.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import BoxDetail from './BoxDetail.svelte'
  import BoxLabel from './BoxLabel.svelte'
  import { bulkBoxes, listBoxes, listRooms, type Box, type Room } from './boxes'
  import { inBoxes, STATUS_LABEL } from './logic'

  let boxes = $state.raw<Box[]>([])
  let rooms = $state.raw<Room[]>([])
  let loaded = $state(false)
  let error = $state('')
  let openId = $state<string | null>(null)
  let open = $state(false)
  const groups = $derived(inBoxes(boxes))
  const canWrite = $derived(can('member'))

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
    listRooms().then((r) => (rooms = r), () => {})
  })
  $effect(() => stream.onBurst(['box', 'item', 'resync'], refresh))

  async function allArrived(list: Box[]) {
    const ids = list.filter((b) => b.box_status === 'packed').map((b) => b.id)
    if (!ids.length) return
    try {
      await bulkBoxes({ ids, box_status: 'arrived' })
      await refresh()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
</script>

<div class="min-h-0 flex-1 overflow-auto bg-surface">
  {#if error}
    <p class="m-0 p-6 text-[14px] text-danger" role="alert">{error}</p>
  {:else if loaded && !groups.length}
    <p class="m-0 p-6 text-[14px] text-muted">No boxes in transit. Everything is unpacked.</p>
  {/if}
  {#each groups as g (g.room?.id ?? 'none')}
    <section class="border-b border-divider p-4 md:px-6" aria-label={g.room?.name ?? 'No room yet'}>
      <div class="mb-3 flex items-center gap-2">
        <h3 class="m-0 flex-1 text-[13px] font-semibold">{g.room?.name ?? 'No room yet'} <span class="font-mono text-muted">{g.boxes.length}</span></h3>
        {#if canWrite && g.boxes.some((b) => b.box_status === 'packed')}<Button onclick={() => allArrived(g.boxes)}>All arrived</Button>{/if}
      </div>
      <ul class="m-0 grid list-none grid-cols-1 gap-4 p-0 min-[480px]:grid-cols-2 md:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]">
        {#each g.boxes as b (b.id)}
          <li>
            <button type="button" class="block w-full cursor-pointer border-0 bg-transparent p-0 text-left" onclick={() => ((openId = b.id), (open = true))}>
              <BoxLabel number={b.box_number} room={b.goes_to} handling={b.handling} />
              <span class="mt-1.5 flex text-[12px] font-semibold">{STATUS_LABEL[b.box_status ?? 'empty']}<span class="ml-auto font-medium text-muted tabular-nums">{b.counts?.items_below ?? 0}</span></span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/each}
</div>

<BoxDetail bind:open boxId={openId} {rooms} onchange={refresh} />

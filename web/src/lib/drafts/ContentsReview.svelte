<script lang="ts">
  // Review the lines the AI read from a container's contents photos (J6.6): edit, delete, add by hand,
  // Make item (more prominent for lines worth their own entry), Make all items, Confirm all.
  import { untrack } from 'svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import { createLine, deleteLine, listLines, patchLine, promoteLine } from '../inventory/places'
  import type { ContentsLine } from '../inventory/types'
  import { stream } from '../stream.svelte'
  import Button from '../ui/Button.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { confirmLines, promoteAllLines, readContents } from './drafts'

  let { placeId }: { placeId: string } = $props()

  let lines = $state<ContentsLine[]>([])
  let loaded = $state(false)
  let reading = $state(false)
  let newName = $state('')
  const aiLines = $derived(lines.filter((l) => l.ai))

  async function refresh(id = placeId) {
    try {
      const p = await listLines(id)
      if (id === placeId) {
        lines = p.data
        loaded = true
      }
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }
  async function read(quiet = false) {
    reading = true
    try {
      await readContents(placeId)
      if (!quiet) showToast('The AI is reading the photos.')
    } catch (e) {
      reading = false
      if (!quiet) showToast(explain(e).message || 'Could not ask the AI. Add the lines by hand.', 'error')
    }
  }
  $effect(() => {
    const id = placeId
    untrack(() => {
      loaded = false
      void refresh(id).then(() => {
        if (lines.length === 0) void read(true)
      })
    })
    return stream.onBurst(
      ['place', 'box', 'job', 'resync'],
      () => {
        reading = false
        void refresh(id)
      },
      (m) => m.type === 'resync' || m.place_id === id || m.id === id,
    )
  })

  const guard = async (fn: () => Promise<unknown>) => {
    try {
      await fn()
    } catch (e) {
      showToast(explain(e).message || 'That did not work.', 'error')
    }
  }
  const rename = (l: ContentsLine, name: string) => {
    const n = name.trim()
    if (n && n !== l.name) void guard(async () => {
      const next = await patchLine(l.id, { name: n })
      lines = lines.map((x) => (x.id === l.id ? next : x))
    })
  }
  const requantity = (l: ContentsLine, q: string) => {
    const n = Math.max(0, Math.floor(Number(q)))
    if (!Number.isNaN(n) && n !== l.quantity) void guard(async () => {
      const next = await patchLine(l.id, { quantity: n })
      lines = lines.map((x) => (x.id === l.id ? next : x))
    })
  }
  const remove = (l: ContentsLine) =>
    guard(async () => {
      await deleteLine(l.id)
      lines = lines.filter((x) => x.id !== l.id)
    })
  const makeItem = (l: ContentsLine) =>
    guard(async () => {
      const it = await promoteLine(l.id)
      lines = lines.filter((x) => x.id !== l.id)
      showToast(`${it.name} is now an item`, 'success')
    })
  const makeAll = () =>
    guard(async () => {
      await promoteAllLines(placeId)
      lines = []
      showToast('Every line is now a draft item', 'success')
    })
  const confirmAll = () =>
    guard(async () => {
      await confirmLines(placeId)
      lines = lines.map((l) => ({ ...l, ai: false }))
    })
  async function add(e: SubmitEvent) {
    e.preventDefault()
    const name = newName.trim()
    if (!name) return
    await guard(async () => {
      lines = [...lines, ...(await createLine(placeId, name)).data]
      newName = ''
    })
  }
  const field = 'h-11 rounded-control border border-border-input bg-surface px-2 text-[16px] md:h-9 md:text-[14px]'
</script>

<div data-testid="contents-review" class="flex flex-col gap-3">
  <div class="flex flex-wrap gap-2">
    <Button icon="sparkle" disabled={reading} onclick={() => read()}>{reading ? 'Reading' : 'Read photos with AI'}</Button>
    <Button icon="check" disabled={!aiLines.length} onclick={confirmAll}>Confirm all</Button>
    <Button icon="tag" disabled={!lines.length} onclick={makeAll}>Make all items</Button>
  </div>
  {#if !loaded}
    <p class="m-0 text-[13px] text-muted">Loading</p>
  {:else if !lines.length}
    <p class="m-0 text-[13px] text-muted">{reading ? 'The AI is reading the photos.' : 'No lines yet. Add them by hand or read the photos.'}</p>
  {/if}
  <ul class="m-0 flex list-none flex-col p-0">
    {#each lines as l (l.id)}
      <li class="flex items-center gap-2 border-b border-divider-row py-2">
        {#if l.ai}<span class="flex-none text-ai-fg" title="Read by AI"><Icon name="sparkle" size={13} /></span>{/if}
        <input class="{field} min-w-0 flex-1" value={l.name} aria-label="Name" onchange={(e) => rename(l, e.currentTarget.value)} />
        <input class="{field} w-16 text-right font-mono" type="number" min="0" value={l.quantity} aria-label="Quantity" onchange={(e) => requantity(l, e.currentTarget.value)} />
        <Button variant={l.worth_item ? 'primary' : 'secondary'} class="h-9 flex-none" onclick={() => makeItem(l)}>Make item</Button>
        <button type="button" class="flex size-9 flex-none items-center justify-center text-muted" aria-label="Remove {l.name}" onclick={() => remove(l)}><Icon name="trash" /></button>
      </li>
    {/each}
  </ul>
  <form class="flex gap-2" onsubmit={add}>
    <input class="{field} min-w-0 flex-1" bind:value={newName} placeholder="Add a line" aria-label="New line" />
    <Button type="submit" icon="plus" disabled={!newName.trim()}>Add</Button>
  </form>
</div>

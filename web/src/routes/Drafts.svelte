<script lang="ts">
  // Draft review queue: drafts on the left, the AI-filled form on the right.
  // Keys: J/K previous and next, Enter confirm and go on, S skip.
  import { untrack } from 'svelte'
  import DraftForm from '../lib/drafts/DraftForm.svelte'
  import { confirmSure, queueState, readWaiting, stateText, isSure } from '../lib/drafts/drafts'
  import { listItems } from '../lib/inventory/items'
  import type { Item } from '../lib/inventory/types'
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import { stream } from '../lib/stream.svelte'
  import Button from '../lib/ui/Button.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  let items = $state.raw<Item[]>([])
  let selected = $state<string | null>(null)
  let form = $state<ReturnType<typeof DraftForm> | null>(null)

  async function refresh() {
    const page = await listItems({ draft: true, sort: 'touched' }, null, 200)
    items = page.data
    if (!selected || !items.some((i) => i.id === selected)) selected = items[0]?.id ?? null
  }
  $effect(() => {
    untrack(() => void refresh().catch(() => showToast('Could not load the drafts.', 'error')))
    return stream.onBurst(['item', 'job', 'resync'], () => void refresh().catch(() => {}))
  })

  const index = $derived(items.findIndex((i) => i.id === selected))
  const go = (d: number) => {
    const n = items[index + d]
    if (n) selected = n.id
  }
  function onkeydown(e: KeyboardEvent) {
    const t = e.target as HTMLElement
    if (t.closest('input, textarea, select') || e.metaKey || e.ctrlKey || e.altKey) return
    const k = e.key.toLowerCase()
    if (k === 'j') go(1)
    else if (k === 'k') go(-1)
    else if (k === 's') go(1)
    else if (k === 'enter' && !t.closest('button')) void form?.confirm()
    else return
    e.preventDefault()
  }
  function confirmed(it: Item) {
    const at = index
    items = items.filter((i) => i.id !== it.id)
    selected = items[Math.min(at, items.length - 1)]?.id ?? null
  }
  async function confirmAll() {
    try {
      await confirmSure()
      await refresh()
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not confirm.', 'error')
    }
  }
  async function readAll() {
    try {
      await readWaiting()
      showToast('Queued the waiting drafts for the AI.')
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not queue them.', 'error')
    }
  }
</script>

<svelte:window {onkeydown} />

<Panel>
  <PageHeader icon="sparkle" title="Drafts">
    {#snippet actions()}
      <Button onclick={readAll}>Read waiting</Button>
      <Button icon="check" disabled={!items.some(isSure)} onclick={confirmAll}>Confirm all sure ones</Button>
    {/snippet}
  </PageHeader>
  <Body>
    <div class="flex h-full min-h-0">
      <ul class="m-0 w-[280px] flex-none list-none overflow-y-auto border-r border-divider p-0" aria-label="Drafts">
        {#each items as it (it.id)}
          <li>
            <button
              type="button"
              class="flex w-full flex-col items-start gap-0.5 border-b border-divider-row px-4 py-3 text-left {it.id === selected ? 'bg-accent-row' : 'hover:bg-surface-subtle'}"
              aria-current={it.id === selected}
              onclick={() => (selected = it.id)}
            >
              <span class="text-[14px] font-medium text-ink">{it.name || 'Untitled'}</span>
              <span class="text-[12px] text-muted">{stateText[queueState(it)]}</span>
            </button>
          </li>
        {:else}
          <li class="p-6 text-center text-[13px] text-muted">No drafts to review.</li>
        {/each}
      </ul>
      <div class="min-w-0 flex-1">
        {#if selected}
          <DraftForm
            bind:this={form}
            id={selected}
            onconfirmed={confirmed}
          />
        {/if}
      </div>
    </div>
  </Body>
</Panel>

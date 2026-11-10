<script lang="ts">
  import { showToast } from '../ui/toasts.svelte'
  import { lookup } from './duplicates'
  import type { Match } from '../search/api'

  interface Props {
    brand?: string
    model?: string
    name?: string
    /** The item being added, never offered as its own duplicate. */
    excludeId: string
    onresolve: (kind: 'same' | 'add', match: Match) => Promise<unknown>
    /** Quantity makes sense for things counted in bulk; hidden otherwise. */
    canAdd?: boolean
  }
  let { brand = '', model = '', name = '', excludeId, onresolve, canAdd = true }: Props = $props()

  let match = $state.raw<Match | null>(null)
  let dismissed = $state('')
  let busy = $state(false)

  // Quiet: a lookup that fails or finds nothing shows nothing and never blocks saving.
  $effect(() => {
    const v = { brand, model, name }
    const t = setTimeout(async () => {
      match = (await lookup(v, excludeId))[0] ?? null
    }, 600)
    return () => clearTimeout(t)
  })

  async function run(kind: 'same' | 'add') {
    if (!match || busy) return
    busy = true
    try {
      await onresolve(kind, match)
      showToast(kind === 'same' ? 'Merged into the item you already have' : 'Added to the quantity', 'success')
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not merge.', 'error')
    } finally {
      busy = false
    }
  }
</script>

{#if match && dismissed !== match.id}
  <div class="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-control bg-accent-soft px-3 py-2 text-[13px] text-body" role="note">
    <span class="min-w-0 flex-1"><b class="font-semibold text-ink">You already have this:</b> {match.title}{match.path ? ` · ${match.path}` : ''}</span>
    <span class="flex gap-1">
      <button type="button" class="cursor-pointer rounded-control px-2 py-1 font-semibold text-accent hover:underline disabled:opacity-50" disabled={busy} onclick={() => run('same')}>Same thing</button>
      <button type="button" class="cursor-pointer rounded-control px-2 py-1 text-muted hover:underline" onclick={() => (dismissed = match!.id)}>Another one</button>
      {#if canAdd}<button type="button" class="cursor-pointer rounded-control px-2 py-1 text-muted hover:underline disabled:opacity-50" disabled={busy} onclick={() => run('add')}>Add to quantity</button>{/if}
    </span>
  </div>
{/if}

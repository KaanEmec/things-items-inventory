<script lang="ts">
  import Icon from '../Icon.svelte'
  import type { TreePlace } from '../inventory/types'
  import { canDropItems } from './logic'
  import { drag } from './drag.svelte'
  import { kindIcon } from './kinds'

  interface Props {
    spots: TreePlace[]
    canWrite: boolean
    phone?: boolean
    onopen: (id: string) => void
    onadd: () => void
    ondropitems: (target: string) => void
  }
  let { spots, canWrite, phone = false, onopen, onadd, ondropitems }: Props = $props()
  let over = $state<string | null>(null)

  function allow(e: DragEvent, id: string) {
    const w = drag.what
    if (w?.kind === 'items' && canDropItems(w.items, id)) (e.preventDefault(), (over = id))
  }
</script>

{#snippet cell(s: TreePlace)}
  {@const n = s.counts?.items_below ?? 0}
  <button
    type="button"
    onclick={() => onopen(s.id)}
    ondragover={(e) => allow(e, s.id)}
    ondragleave={() => over === s.id && (over = null)}
    ondrop={(e) => (e.preventDefault(), (over = null), ondropitems(s.id))}
    class="flex cursor-pointer text-left {phone
      ? 'min-h-[76px] flex-col justify-between gap-2 rounded-[12px] border border-border bg-surface p-3 hover:bg-surface-subtle'
      : 'h-[68px] min-w-[132px] flex-[1_1_132px] flex-col justify-between border-r border-b border-divider bg-surface p-3 hover:bg-surface-subtle'} {over === s.id ? 'bg-accent-soft outline-2 -outline-offset-2 outline-accent' : ''}"
  >
    <span class="flex items-center gap-1.5 text-[13px] font-semibold">
      <Icon name={kindIcon[s.kind] ?? 'drawer'} class="text-label" /><span class="truncate">{s.name}</span>
    </span>
    <span class="font-mono text-[12px] {n ? 'text-body' : 'text-faint'}">{n} {n === 1 ? 'item' : 'items'}{#if s.counts?.lines}<span class="text-muted"> · {s.counts.lines} loose</span>{/if}</span>
  </button>
{/snippet}

{#if phone}
  <div class="grid grid-cols-2 gap-2.5 px-4 py-3">
    {#each spots as s (s.id)}{@render cell(s)}{/each}
    {#if canWrite}
      <button type="button" onclick={onadd} class="flex min-h-[76px] cursor-pointer items-center justify-center gap-1.5 rounded-[12px] border border-dashed border-border-input text-[14px] font-semibold text-accent"><Icon name="plus" />Add spots</button>
    {/if}
  </div>
{:else}
  <!-- Connected cells sharing borders, as one strip. -->
  <div class="flex flex-wrap border-t border-l border-divider">
    {#each spots as s (s.id)}{@render cell(s)}{/each}
    {#if canWrite}
      <button type="button" onclick={onadd} class="flex h-[68px] min-w-[132px] flex-[1_1_132px] cursor-pointer items-center justify-center gap-1.5 border-r border-b border-divider bg-surface-subtle text-[13px] font-semibold text-accent hover:bg-accent-soft">
        <Icon name="plus" />Add spots
      </button>
    {/if}
  </div>
{/if}

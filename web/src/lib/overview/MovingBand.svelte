<script lang="ts">
  import Icon from '../Icon.svelte'
  import { roomColor } from '../places/kinds'
  import type { Moving } from './types'

  let { moving }: { moving: Moving } = $props()
  const packed = $derived(moving.rooms.reduce((n, r) => n + r.packed, 0))
  const pct = $derived(moving.total ? Math.round((packed / moving.total) * 100) : 0)
</script>

<!-- Moving summary: packed share plus the boxes per destination room. -->
<a href="/moving" class="flex flex-wrap items-center gap-x-6 gap-y-3 border-b border-divider bg-surface px-4 py-3.5 hover:bg-surface-subtle md:px-6">
  <span class="flex items-center gap-2 text-[13px] font-semibold"><Icon name="truck" class="text-label" />Moving</span>
  <span class="flex min-w-[200px] flex-[1_1_240px] items-center gap-3">
    <span class="font-mono text-[20px] font-semibold">{pct}%</span>
    <span class="min-w-0 flex-1">
      <span class="block h-1.5 overflow-hidden rounded-full bg-chrome" role="progressbar" aria-valuenow={pct} aria-valuemin="0" aria-valuemax="100" aria-label="Boxes packed">
        <span class="block h-full rounded-full bg-accent" style:width="{pct}%"></span>
      </span>
      <span class="mt-1 block text-[12px] text-muted"><span class="font-mono">{packed}</span> of <span class="font-mono">{moving.total}</span> {moving.total === 1 ? 'box' : 'boxes'} packed · <span class="font-mono">{moving.by_status.open ?? 0}</span> open{#if moving.by_status.arrived} · <span class="font-mono">{moving.by_status.arrived}</span> to unpack{/if}</span>
    </span>
  </span>
  <span class="flex flex-wrap gap-x-4 gap-y-1.5">
    {#each moving.rooms as r (r.room_id ?? 'none')}
      <span class="inline-flex items-center gap-1.5 text-[12px] font-semibold tracking-[0.04em] uppercase">
        <span class="size-2.5 rounded-full {r.color && roomColor[r.color] ? '' : 'bg-label'}" style:background={r.color ? roomColor[r.color] : undefined}></span>
        <span class="font-mono text-[13px]">{r.total}</span>{r.name ?? 'No room'}
      </span>
    {/each}
  </span>
</a>

<script lang="ts">
  import type { Counts } from './types'
  import { stripCells } from './logic'

  let { counts }: { counts: Counts } = $props()
  const cells = $derived(stripCells(counts))
</script>

<!-- Connected cells sharing borders; each opens the Items list with the matching filter. -->
<nav aria-label="Inventory status" class="grid grid-cols-3 border-b border-divider bg-surface md:grid-cols-5">
  {#each cells as c, i (c.key)}
    <a
      href={c.href}
      class="flex min-w-0 flex-col justify-center gap-0.5 px-4 py-3 hover:bg-surface-subtle focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent md:px-6 md:py-4
        {i % 3 !== 2 ? 'max-md:border-r max-md:border-divider' : ''} {i < 3 ? 'max-md:border-b max-md:border-divider' : ''} {i < 4 ? 'md:border-r md:border-divider' : ''}"
    >
      <span class="font-mono text-[24px] leading-none font-semibold tracking-[-0.02em] md:text-[28px]">{c.value}</span>
      <span class="truncate text-[12px] text-muted md:text-[13px]">{c.label}</span>
    </a>
  {/each}
</nav>

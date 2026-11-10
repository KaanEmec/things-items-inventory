<script lang="ts">
  import type { Snippet } from 'svelte'

  /** Phone grouped list: edge-to-edge white block with hairline dividers, optional small caps title. */
  let { title, children }: { title?: string; children: Snippet } = $props()
</script>

{#if title}
  <h3 class="m-0 px-4 pt-[18px] pb-2 text-[11px] font-semibold tracking-[0.08em] text-label uppercase md:px-6">{title}</h3>
{/if}
<div class="divide-y divide-divider-row border-y border-divider bg-surface first:border-t-0">{@render children()}</div>

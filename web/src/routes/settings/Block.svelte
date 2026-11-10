<script lang="ts">
  import type { Snippet } from 'svelte'

  /** One titled group inside a Settings section: grouped list on phone, docked band on desktop. */
  let { title, note, children }: { title: string; note?: string; children: Snippet } = $props()
</script>

<div class="border-b border-divider bg-surface max-md:mb-3 max-md:border-t">
  <div class="px-4 py-4 md:max-w-2xl md:px-6">
    <h3 class="m-0 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">{title}</h3>
    {#if note}<p class="m-0 mt-1 text-[13px] text-muted">{note}</p>{/if}
    <div class="mt-3">{@render children()}</div>
  </div>
</div>

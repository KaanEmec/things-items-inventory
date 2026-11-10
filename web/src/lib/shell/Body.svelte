<script lang="ts">
  import type { Snippet } from 'svelte'

  let { children }: { children?: Snippet } = $props()
</script>

<!-- Scrolls inside the panel. Sunken grey on phone so grouped lists read edge to edge. -->
<div class="min-h-0 flex-1 overflow-auto bg-surface-sunken md:bg-surface">
  {@render children?.()}
</div>

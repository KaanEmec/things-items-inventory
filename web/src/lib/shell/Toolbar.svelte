<script lang="ts">
  import type { Snippet } from 'svelte'

  let { children }: { children: Snippet } = $props()
</script>

<div class="flex min-h-11 flex-none flex-wrap items-center gap-2 border-b border-divider bg-surface-subtle px-4 py-1 md:px-6">
  {@render children()}
</div>

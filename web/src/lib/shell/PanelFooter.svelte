<script lang="ts">
  import type { Snippet } from 'svelte'

  let { children }: { children: Snippet } = $props()
</script>

<footer class="hidden min-h-9 flex-none flex-wrap items-center gap-x-[18px] border-t border-divider bg-surface-subtle px-6 text-[12px] text-muted md:flex">
  {@render children()}
</footer>

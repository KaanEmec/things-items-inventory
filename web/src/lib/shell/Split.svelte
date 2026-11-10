<script lang="ts">
  import type { Snippet } from 'svelte'

  /** Two docked panes joined by a 1px divider (list | detail). Stacks when narrow. */
  let { main, side }: { main: Snippet; side: Snippet } = $props()
</script>

<div class="flex min-h-0 flex-1 flex-wrap overflow-auto">
  <section class="flex min-w-0 flex-[2_1_540px] flex-col border-r border-divider">{@render main()}</section>
  <aside class="flex min-w-0 flex-[1_1_300px] flex-col">{@render side()}</aside>
</div>

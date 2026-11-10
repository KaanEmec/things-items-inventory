<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'

  let { icon, title, children }: { icon: IconName; title: string; children?: Snippet } = $props()
</script>

<div class="flex h-11 flex-none items-center gap-2 border-b border-divider bg-surface-subtle px-4 md:px-6">
  <Icon name={icon} class="text-label" />
  <h2 class="m-0 text-[13px] font-semibold">{title}</h2>
  {#if children}<div class="ml-auto flex items-center gap-2 text-[13px]">{@render children()}</div>{/if}
</div>

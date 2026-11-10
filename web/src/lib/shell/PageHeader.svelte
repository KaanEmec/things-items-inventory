<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'

  interface Props {
    icon: IconName
    title: string
    breadcrumb?: string[]
    actions?: Snippet
    class?: string
  }
  let { icon, title, breadcrumb, actions, class: cls = '' }: Props = $props()
</script>

<header
  class="flex min-h-14 flex-none flex-wrap items-center gap-x-4 gap-y-3 border-b border-divider bg-surface px-4 py-2 max-md:pt-[calc(env(safe-area-inset-top)+8px)] md:min-h-[70px] md:px-6 md:py-3.5 {cls}"
>
  <span class="hidden size-[38px] flex-none items-center justify-center rounded-tile bg-accent-soft text-accent md:flex">
    <Icon name={icon} size={20} />
  </span>
  <div class="min-w-0 flex-[1_1_200px]">
    {#if breadcrumb?.length}
      <nav aria-label="Breadcrumb" class="flex items-center gap-1 text-[12px] text-muted">
        {#each breadcrumb as part, i}
          {#if i > 0}<Icon name="chevron-right" size={11} />{/if}{part}
        {/each}
      </nav>
    {/if}
    <h1 class="m-0 text-[18px] font-bold tracking-[-0.01em]">{title}</h1>
  </div>
  {#if actions}<div class="flex items-center gap-2">{@render actions()}</div>{/if}
</header>

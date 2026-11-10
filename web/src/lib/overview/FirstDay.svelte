<script lang="ts">
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'

  let { canWrite, hasPlaces }: { canWrite: boolean; hasPlaces: boolean } = $props()
  const steps: { icon: IconName; title: string; text: string; href: string }[] = $derived([
    { icon: 'pin', title: 'Add a place', text: 'Name the rooms and drawers where things live, like "3rd drawer from top · left closet".', href: '/places' },
    { icon: 'tag', title: 'Add an item', text: 'Type a name, pick a place. Details can wait.', href: '/items?new=1' },
    { icon: 'camera', title: 'Capture a photo', text: 'Photograph a thing or a box of things; the details are filled in for you.', href: '/capture' },
  ])
</script>

<section aria-label="Getting started" class="border-b border-divider bg-surface">
  <div class="px-4 pt-6 pb-4 md:px-6">
    <h2 class="m-0 text-[20px] font-bold tracking-[-0.01em]">Start your inventory</h2>
    <p class="mt-1 mb-0 max-w-[60ch] text-[14px] text-muted">
      {#if canWrite}Nothing is here yet. Pick one to begin{hasPlaces ? '' : '; places first makes the rest easier'}.{:else}Nothing is here yet. Ask a member of the household to add the first places and items.{/if}
    </p>
  </div>
  {#if canWrite}
    <ol class="m-0 list-none divide-y divide-divider-row border-t border-divider p-0">
      {#each steps as s, i (s.href)}
        <li>
          <a href={s.href} class="flex min-h-16 items-center gap-4 px-4 py-3 hover:bg-surface-subtle md:px-6">
            <span class="inline-flex size-10 flex-none items-center justify-center rounded-tile bg-accent-soft text-accent"><Icon name={s.icon} size={20} /></span>
            <span class="min-w-0 flex-1">
              <b class="block text-[15px] font-semibold"><span class="mr-1.5 font-mono text-muted">{i + 1}</span>{s.title}</b>
              <span class="block text-[13px] text-muted">{s.text}</span>
            </span>
            <Icon name="chevron-right" class="flex-none text-label" />
          </a>
        </li>
      {/each}
    </ol>
  {/if}
</section>

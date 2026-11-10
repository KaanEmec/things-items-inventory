<script lang="ts">
  import Icon from '../Icon.svelte'
  import Avatar from '../ui/Avatar.svelte'
  import { entryHref, phrase, shortAgo } from './logic'
  import type { RecentEntry } from './types'

  let { entries }: { entries: RecentEntry[] } = $props()
</script>

{#if entries.length}
  <ul class="m-0 list-none divide-y divide-divider-row p-0">
    {#each entries as e (e.id)}
      {@const p = phrase(e)}
      {@const href = entryHref(e)}
      <li>
        <svelte:element
          this={href ? 'a' : 'div'}
          {href}
          class="flex min-h-14 items-center gap-3 px-4 py-2 md:px-6 {href ? 'hover:bg-surface-subtle' : ''}"
        >
          {#if e.actor.kind === 'ai'}
            <span class="inline-flex size-[26px] flex-none items-center justify-center rounded-full bg-ai-bg text-ai-fg" role="img" aria-label="AI"><Icon name="sparkle" size={14} /></span>
          {:else}
            <Avatar name={e.actor.name} size={26} />
          {/if}
          <span class="min-w-0 flex-1 text-[14px] leading-snug">
            <span class="text-muted">{e.actor.name} {p.verb}</span>
            <b class="font-semibold">{p.target}</b>
            {#if p.tail}<span class="text-muted">{p.tail}</span>{/if}
          </span>
          <time datetime={e.at} class="flex-none font-mono text-[12px] text-muted">{shortAgo(e.at)}</time>
        </svelte:element>
      </li>
    {/each}
  </ul>
{:else}
  <p class="m-0 px-4 py-6 text-[14px] text-muted md:px-6">Changes show up here as people add and move things.</p>
{/if}

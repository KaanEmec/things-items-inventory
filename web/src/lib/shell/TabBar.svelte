<script lang="ts">
  import Icon from '../Icon.svelte'
  import { captureQueue } from '../capture/queue.svelte'
  import { isActive } from '../router.svelte'
  import { routes } from '../../routes'

  const tabs = routes.filter((r) => r.tab)
</script>

<nav
  aria-label="Main"
  class="grid h-[84px] flex-none grid-cols-5 items-start border-t border-divider bg-surface px-1.5 pt-1.5 pb-[max(env(safe-area-inset-bottom),10px)] md:hidden"
>
  {#each tabs as r (r.path)}
    {#if r.path === '/capture'}
      <a href={r.path} aria-label={r.title} class="flex min-h-12 justify-center">
        <span class="relative -mt-[22px] flex size-14 items-center justify-center rounded-[18px] border-[3px] border-surface bg-accent text-white shadow-capture">
          <Icon name={r.icon} size={22} />
          {#if captureQueue.pending + captureQueue.failed > 0}
            <span
              class="absolute -top-2 -right-2 flex h-5 min-w-5 items-center justify-center rounded-full border-2 border-surface px-1 font-mono text-[11px] font-semibold text-white {captureQueue.failed ? 'bg-danger' : 'bg-ink'}"
              aria-label="{captureQueue.pending} photos to upload{captureQueue.failed ? `, ${captureQueue.failed} failed` : ''}"
            >{captureQueue.pending + captureQueue.failed}</span>
          {/if}
        </span>
      </a>
    {:else}
      <a
        href={r.path}
        aria-current={isActive(r.path) ? 'page' : undefined}
        class="flex min-h-12 flex-col items-center gap-[3px] pt-1.5 text-[11px] font-semibold text-label aria-[current=page]:text-accent"
      >
        <Icon name={r.icon} size={22} />{r.tab}
      </a>
    {/if}
  {/each}
</nav>

<script lang="ts">
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'
  import CategoryTile from '../ui/CategoryTile.svelte'
  import { daysUntil, groupMeta, shortAgo, visibleGroups } from './logic'
  import type { NeedsGroup, NeedsKind, NeedsRow } from './types'

  let { groups }: { groups: NeedsGroup[] } = $props()
  const shown = $derived(visibleGroups(groups))

  const icons: Record<NeedsKind, IconName> = { drafts: 'sparkle', warranty_ending: 'receipt', unseen: 'clock', failed_jobs: 'refresh' }

  function detail(kind: NeedsKind, r: NeedsRow): string {
    switch (kind) {
      case 'warranty_ending': {
        const d = r.at ? daysUntil(r.at) : null
        return d === null ? '' : d === 0 ? 'ends today' : `ends in ${d} d`
      }
      case 'unseen':
        return [r.at ? `seen ${shortAgo(r.at)} ago` : '', r.place_path].filter(Boolean).join(' · ')
      case 'failed_jobs':
        return r.detail ?? 'Failed'
      default:
        return r.place_path ?? 'Not placed'
    }
  }
  const href = (r: NeedsRow, kind: NeedsKind) => (r.item_id ? `/items/${r.item_id}` : kind === 'failed_jobs' ? '/settings/ai' : null)
</script>

{#if shown.length}
  {#each shown as g (g.kind)}
    <div class="flex min-h-11 items-center gap-2 border-b border-divider-row bg-surface px-4 pt-2 md:px-6 {g.kind === shown[0].kind ? '' : 'border-t border-t-divider'}">
      <Icon name={icons[g.kind]} size={15} class={g.kind === 'drafts' ? 'text-ai-fg' : 'text-label'} />
      <h3 class="m-0 min-w-0 flex-1 truncate pb-2 text-[13px] font-semibold"><span class="font-mono">{g.count}</span>{groupMeta[g.kind].title(g.count).slice(String(g.count).length)}</h3>
      {#if groupMeta[g.kind].href}
        <a href={groupMeta[g.kind].href} class="pb-2 text-[12px] font-medium text-accent hover:underline">See all</a>
      {/if}
    </div>
    <ul class="m-0 list-none divide-y divide-divider-row p-0">
      {#each g.rows as r (r.id)}
        {@const to = href(r, g.kind)}
        <li>
          <svelte:element this={to ? 'a' : 'div'} href={to} class="flex min-h-14 items-center gap-3 px-4 py-2 md:px-6 {to ? 'hover:bg-surface-subtle' : ''}">
            {#if r.category}<CategoryTile category={r.category} size={32} />{:else}<span class="inline-flex size-8 flex-none items-center justify-center rounded-[9px] bg-ai-bg text-ai-fg"><Icon name="sparkle" size={16} /></span>{/if}
            <span class="min-w-0 flex-1">
              <b class="block truncate text-[14px] font-semibold">{r.name}</b>
              <span class="block truncate text-[12px] {g.kind === 'warranty_ending' ? 'font-mono' : ''} text-muted">{detail(g.kind, r)}</span>
            </span>
            <Icon name="chevron-right" size={14} class="flex-none text-label" />
          </svelte:element>
        </li>
      {/each}
      {#if g.count > g.rows.length}
        <li class="px-4 py-2 text-[12px] text-muted md:px-6">and <span class="font-mono">{g.count - g.rows.length}</span> more</li>
      {/if}
    </ul>
  {/each}
{:else}
  <div class="flex items-center gap-3 px-4 py-6 text-[14px] text-muted md:px-6">
    <span class="inline-flex size-8 flex-none items-center justify-center rounded-full bg-use-bg text-use-fg"><Icon name="check" size={16} /></span>
    Nothing needs a look. Drafts, warranties and daily-use items are all in order.
  </div>
{/if}

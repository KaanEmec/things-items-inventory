<script lang="ts">
  import type { AIStatus } from '../../lib/ai.svelte'
  import { shortDate } from '../../lib/format'
  import Icon from '../../lib/Icon.svelte'

  let { status, since }: { status: AIStatus; since?: string | null } = $props()
  const look = {
    working: ['bg-use-bg text-use-fg', 'check', 'Working'],
    not_set: ['bg-stored-bg text-stored-fg', 'clock', 'Not set'],
    failing: ['bg-repair-bg text-repair-fg', 'x', 'Failing'],
    paused: ['bg-sell-bg text-sell-fg', 'clock', 'Paused'],
  } as const
  const l = $derived(look[status])
</script>

<span class="inline-flex h-6 flex-none items-center gap-1 rounded-full px-2.5 text-[12px] font-semibold {l[0]}">
  <Icon name={l[1]} size={12} />{l[2]}{#if status === 'failing' && since}&nbsp;since {shortDate(since)}{/if}
</span>

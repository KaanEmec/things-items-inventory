<script lang="ts">
  import { featureLabel, formatCost, formatTokens, getUsage, listActivity, type AIActivity, type AIRole, type AIUsage } from '../../lib/ai.svelte'
  import { explain } from '../../lib/errors'
  import { ago } from '../../lib/format'
  import Icon from '../../lib/Icon.svelte'
  import Button from '../../lib/ui/Button.svelte'

  let rows = $state.raw<AIActivity[]>([])
  let next = $state<string | null>(null)
  let loaded = $state(false)
  let loading = $state(false)
  let error = $state('')
  let usage = $state<{ month: string; roles: Record<AIRole, AIUsage> } | null>(null)

  $effect(() => {
    listActivity().then(
      (p) => {
        rows = p.data
        next = p.next_cursor
        loaded = true
      },
      (e) => {
        error = explain(e).message
        loaded = true
      },
    )
    getUsage().then((u) => (usage = u), () => {})
  })

  async function more() {
    if (!next || loading) return
    loading = true
    try {
      const p = await listActivity(next)
      rows = [...rows, ...p.data]
      next = p.next_cursor
    } catch (e) {
      error = explain(e).message
    } finally {
      loading = false
    }
  }

  const roleName: Record<AIRole, string> = { vision: 'Photo understanding', text: 'Everything else' }
  const monthLabel = $derived(usage ? new Date(usage.month + '-01T12:00:00').toLocaleDateString(undefined, { month: 'long', year: 'numeric' }) : '')
  const exact = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  const tokens = (r: AIActivity) =>
    [r.tokens_in != null ? formatTokens(r.tokens_in) + ' in' : '', r.tokens_out != null ? formatTokens(r.tokens_out) + ' out' : ''].filter(Boolean).join(' · ')
</script>

<section class="border-b border-divider bg-surface max-md:mb-3 max-md:border-t" aria-label="AI activity">
  <div class="flex min-h-12 items-center gap-2 border-b border-divider bg-surface-subtle px-4 md:px-6">
    <Icon name="pulse" class="text-label" />
    <h3 class="m-0 flex-1 text-[13px] font-semibold">Activity</h3>
  </div>

  {#if usage}
    <ul class="m-0 flex list-none flex-col gap-1 border-b border-divider-row px-4 py-3 text-[13px] md:px-6" aria-label="Use this month">
      {#each ['vision', 'text'] as const as role (role)}
        {@const u = usage.roles[role]}
        <li>
          <b class="font-semibold">{roleName[role]}</b>
          <span class="text-muted">
            {monthLabel}: {u.requests} {u.requests === 1 ? 'request' : 'requests'}, {u.failures} failed, {formatTokens(u.tokens_in + u.tokens_out) || '0'} tokens{formatCost(u.cost) ? ', ' + formatCost(u.cost) : ''}
          </span>
        </li>
      {/each}
    </ul>
  {/if}

  {#if error && !rows.length}
    <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6" role="alert">{error}</p>
  {:else if !loaded}
    <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">Loading</p>
  {:else if !rows.length}
    <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">No AI requests yet.</p>
  {:else}
    <ol class="m-0 flex list-none flex-col p-0">
      {#each rows as r (r.id)}
        <li class="flex items-start gap-3 border-b border-divider-row px-4 py-2.5 text-[13px] last:border-b-0 md:px-6">
          <Icon name={r.ok ? 'check' : 'x'} class="mt-0.5 flex-none {r.ok ? 'text-use-fg' : 'text-repair-fg'}" />
          <div class="min-w-0 flex-1">
            <div><b class="font-semibold">{featureLabel(r.feature)}</b> <span class="text-muted">{r.ok ? 'worked' : 'failed'}</span></div>
            <div class="truncate font-mono text-[12px] text-muted">{[r.model, tokens(r), formatCost(r.cost)].filter(Boolean).join(' · ')}</div>
            {#if !r.ok}
              <div class="text-repair-fg">
                {r.error ?? 'The request failed.'}
                {#if r.item_id}<a class="underline" href="/items/{r.item_id}">Open the item</a>{/if}
              </div>
            {/if}
          </div>
          <time datetime={r.at} title={exact(r.at)} class="flex-none text-[12px] text-muted">{ago(r.at)}</time>
        </li>
      {/each}
    </ol>
    {#if next}
      <div class="border-t border-divider-row px-4 py-2 md:px-6"><Button onclick={more} disabled={loading}>{loading ? 'Loading' : 'Load more'}</Button></div>
    {/if}
  {/if}
</section>

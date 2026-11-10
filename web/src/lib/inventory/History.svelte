<script lang="ts">
  import { auth } from '../auth.svelte'
  import { explain } from '../errors'
  import { api, type Page } from '../api'
  import { ago } from '../format'
  import Icon from '../Icon.svelte'
  import { stream } from '../stream.svelte'
  import Avatar from '../ui/Avatar.svelte'
  import Button from '../ui/Button.svelte'
  import { actorLabel, verb, type HistoryEntry } from './history'

  let { itemId }: { itemId: string } = $props()

  const FIRST = 5
  const MORE = 20
  let entries = $state.raw<HistoryEntry[]>([])
  let next = $state<string | null>(null)
  let loaded = $state(false)
  let loading = $state(false)
  let error = $state('')
  let now = $state(Date.now())
  let run = 0

  const url = (cursor: string | null, limit: number) =>
    `/api/v1/items/${itemId}/history?limit=${limit}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`

  /** Loads the newest events again, keeping as many rows as are already shown. */
  async function load(keep = FIRST) {
    const r = ++run
    const id = itemId
    try {
      const page = await api.get<Page<HistoryEntry>>(url(null, Math.min(100, Math.max(FIRST, keep))))
      if (r !== run || id !== itemId) return
      entries = page.data
      next = page.next_cursor
      error = ''
    } catch (e) {
      if (r === run) error = explain(e).message
    } finally {
      if (r === run) loaded = true
    }
  }

  async function more() {
    if (!next || loading) return
    loading = true
    const r = run
    try {
      const page = await api.get<Page<HistoryEntry>>(url(next, MORE))
      if (r !== run) return
      entries = [...entries, ...page.data.filter((e) => !entries.some((x) => x.id === e.id))]
      next = page.next_cursor
    } catch (e) {
      error = explain(e).message
    } finally {
      loading = false
    }
  }

  $effect(() => {
    itemId
    entries = []
    next = null
    loaded = false
    load()
  })

  $effect(() => {
    const id = itemId
    const off = stream.onBurst(
      ['item', 'resync'],
      () => load(entries.length),
      (m) => m.type === 'resync' || m.id === id,
    )
    const tick = setInterval(() => (now = Date.now()), 60000)
    return () => {
      off()
      clearInterval(tick)
    }
  })

  const exact = (iso: string) => new Date(iso).toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit' })
</script>

<section aria-label="History">
  <h3 class="m-0 flex h-11 items-center gap-2 border-b border-divider bg-surface-subtle px-4 text-[13px] font-semibold md:px-6"><Icon name="clock" class="text-label" />History</h3>
  <div class="border-b border-divider bg-surface">
    {#if error && !entries.length}
      <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6" role="alert">{error}</p>
    {:else if !loaded}
      <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">Loading</p>
    {:else if !entries.length}
      <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">Nothing recorded yet.</p>
    {:else}
      <ol class="m-0 flex list-none flex-col p-0">
        {#each entries as e (e.id)}
          <li class="flex items-start gap-3 border-b border-divider-row px-4 py-2.5 last:border-b-0 md:px-6">
            {#if e.actor.kind === 'ai'}
              <span class="inline-flex size-[22px] flex-none items-center justify-center rounded-full bg-chrome text-label" role="img" aria-label="AI"><Icon name="sparkle" size={13} /></span>
            {:else}
              <Avatar name={e.actor.name || '?'} size={22} />
            {/if}
            <div class="min-w-0 flex-1 text-[13px]">
              <span><b class="font-semibold">{actorLabel(e.actor, auth.user?.id)}</b> {verb(e)}</span>
              {#if e.actor.kind === 'token' && e.actor.token_name}<span class="text-muted"> · via {e.actor.token_name}</span>{/if}
            </div>
            <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
            <time datetime={e.at} tabindex="0" aria-label="{ago(e.at, now)}, {exact(e.at)}" class="group relative flex-none text-[12px] text-muted outline-offset-2">
              {ago(e.at, now)}
              <span role="tooltip" class="pointer-events-none absolute top-full right-0 z-10 mt-1 hidden rounded-control border border-border bg-surface px-2 py-1 text-[12px] whitespace-nowrap text-body shadow-button group-hover:block group-focus:block">{exact(e.at)}</span>
            </time>
          </li>
        {/each}
      </ol>
      {#if next}
        <div class="border-t border-divider-row px-4 py-2 md:px-6"><Button onclick={more} disabled={loading}>{loading ? 'Loading' : 'Show more'}</Button></div>
      {/if}
      {#if error}<p class="m-0 px-4 pb-2 text-[12px] text-muted md:px-6" role="alert">{error}</p>{/if}
    {/if}
  </div>
</section>

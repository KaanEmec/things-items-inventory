<script lang="ts">
  import AiHint from '../lib/AiHint.svelte'
  import { ai } from '../lib/ai.svelte'
  import { activeHousehold } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import Icon from '../lib/Icon.svelte'
  import { getCatalog, categoryLabel, valueLabel } from '../lib/inventory/categories'
  import PlaceDialog from '../lib/inventory/PlaceDialog.svelte'
  import { getTree, listHomes, recentPlaces, rememberPlace } from '../lib/inventory/places'
  import PackingBar from '../lib/moving/PackingBar.svelte'
  import { boxPlace, packing } from '../lib/moving/packing.svelte'
  import DescribeBox from '../lib/quickadd/DescribeBox.svelte'
  import { Describe as DescribeModel } from '../lib/quickadd/textrows.svelte'
  import DupHint from '../lib/quickadd/DupHint.svelte'
  import { Sheet } from '../lib/quickadd/sheet.svelte'
  import { captureQueue } from '../lib/capture/queue.svelte'
  import { offlineApi } from '../lib/quickadd/offline'
  import { toJpeg } from '../lib/inventory/photos'
  import { createSuggest } from '../lib/quickadd/suggest'
  import FlowFrame from '../lib/shell/FlowFrame.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import Skeleton from '../lib/ui/Skeleton.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  let sheet = $state.raw<Sheet>()
  const describe = new DescribeModel()
  let error = $state('')
  let attempt = $state(0)
  let placeOpen = $state(false)
  let describing = $state(false)
  let name = $state('')
  let input: HTMLInputElement | undefined = $state()

  $effect(() => {
    if (sheet) return
    void attempt
    void ai.ensure()
    getCatalog().then(
      (catalog) => {
        const s = new Sheet(catalog, { api: offlineApi(captureQueue), suggest: createSuggest(), sendAiFields: true })
        // Start where the person last put something (J7.6); a packing box overrides it below.
        const last = recentPlaces(activeHousehold()?.id ?? '')[0]
        if (last) s.setPlaceDefault(last)
        sheet = s
        loadPlaces(s)
      },
      (e) => (error = explain(e).message),
    )
  })

  $effect(() => {
    if (!packing.loaded) packing.load().catch(() => {})
  })
  $effect(() => {
    const s = sheet
    const b = packing.box
    if (s && b) s.setPlaceDefault(boxPlace(b))
  })

  async function loadPlaces(s: Sheet) {
    try {
      const homes = await listHomes()
      for (const h of homes.data) s.setPlaces((await getTree(h.id)).places)
    } catch {
      // picking still works
    }
  }

  $effect(() => {
    const s = sheet
    if (!s) return
    const flush = () => void s.flushAll()
    addEventListener('pagehide', flush)
    return () => {
      removeEventListener('pagehide', flush)
      flush()
    }
  })

  const recents = $derived(sheet ? recentPlaces(activeHousehold()?.id ?? '').filter((p) => p.id !== sheet!.defaults.place).slice(0, 4) : [])

  function pick(p: import('../lib/inventory/types').PlaceShort | null) {
    sheet!.setPlaceDefault(p)
    if (p) rememberPlace(activeHousehold()?.id ?? '', p)
  }

  // Add keeps focus, so the keyboard stays up for the next line.
  function add(e: SubmitEvent) {
    e.preventDefault()
    if (sheet?.addTyped(name)) name = ''
    input?.focus()
  }

  function remove(id: string) {
    const s = sheet!
    if (!s.deleteById(id)) return
    showToast('Line deleted', 'info', 4000, { label: 'Undo', run: () => s.undo() })
  }

  // A photo for a line goes through the capture queue: it survives being offline and starts the draft pipeline.
  async function photo(id: string, file: File | undefined) {
    if (!file) return
    try {
      await captureQueue.add({ groupId: id, role: 'item', placeId: null, blob: await toJpeg(file) })
      showToast('Photo added. It uploads when there is a connection.', 'info', 3000)
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not add the photo.', 'error')
    }
  }

  // Swipe a line to the left to delete it; the delete button does the same.
  let swipe = $state<{ id: string; x0: number; dx: number } | null>(null)
  function swipeEnd(id: string) {
    const done = swipe?.id === id && swipe.dx < -90
    swipe = null
    if (done) remove(id)
  }

  const chip = 'cursor-pointer rounded-full border border-border-input bg-accent-soft px-2.5 py-1 text-[12px] text-accent'
  const field = 'h-9 rounded-control border border-border-input bg-surface px-2 text-[14px] text-ink'
</script>

<FlowFrame icon="table" title="Quick add">
  {#if sheet}
    {@const s = sheet}
    <div class="sticky top-0 z-10 flex flex-col gap-2 border-b border-divider bg-surface px-4 py-3">
      <div class="flex items-center gap-2">
        <button type="button" class="{field} flex min-w-0 flex-1 cursor-pointer items-center gap-1.5" onclick={() => (placeOpen = true)}>
          <Icon name="pin" size={14} class="text-label" />
          <span class="truncate">{s.places[s.defaults.place]?.name ?? 'Not placed'}</span>
        </button>
        <select class={field} aria-label="Status" value={s.defaults.status} onchange={(e) => s.setDefaults({ status: e.currentTarget.value })}>
          {#each s.catalog.statuses as st (st)}<option value={st}>{valueLabel(st)}</option>{/each}
        </select>
      </div>
      {#if recents.length}
        <div class="flex flex-wrap gap-1.5" aria-label="Recent places">
          {#each recents as p (p.id)}<button type="button" class={chip} onclick={() => pick(p)}>{p.name}</button>{/each}
        </div>
      {/if}
      <AiHint role="text" />
    </div>

    {#if describing}<DescribeBox {describe} sheet={s} hint />{/if}

    <ul class="m-0 flex list-none flex-col p-0">
      {#each s.named as r (r.id)}
        {@const qty = Number(r.values.quantity) || 1}
        <li
          class="flex flex-col gap-2 border-b border-divider bg-surface px-4 py-3 [touch-action:pan-y]"
          style:transform={swipe?.id === r.id ? `translateX(${Math.min(0, swipe.dx)}px)` : undefined}
          onpointerdown={(e) => e.pointerType === 'touch' && (swipe = { id: r.id, x0: e.clientX, dx: 0 })}
          onpointermove={(e) => swipe?.id === r.id && (swipe.dx = e.clientX - swipe.x0)}
          onpointerup={() => swipeEnd(r.id)}
          onpointercancel={() => (swipe = null)}
        >
          <div class="flex items-center gap-2">
            {#if r.created}
              <a href="/items/{r.id}" class="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{r.values.name}</a>
            {:else}
              <span class="min-w-0 flex-1 truncate text-[15px] font-semibold text-ink">{r.values.name}</span>
            {/if}
            <span class="flex items-center gap-1" role="group" aria-label="Quantity">
              <button type="button" class="size-9 cursor-pointer rounded-control border border-border-input text-[16px]" aria-label="Fewer" disabled={qty <= 1} onclick={() => s.setValue(r.id, 'quantity', String(qty - 1))}>−</button>
              <span class="w-6 text-center font-mono text-[14px]">{qty}</span>
              <button type="button" class="size-9 cursor-pointer rounded-control border border-border-input text-[16px]" aria-label="More" onclick={() => s.setValue(r.id, 'quantity', String(qty + 1))}>+</button>
            </span>
            <label class="flex size-9 cursor-pointer items-center justify-center text-muted" aria-label="Add a photo to {r.values.name}">
              <Icon name="camera" size={16} />
              <input type="file" accept="image/*" capture="environment" class="hidden" onchange={(e) => (photo(r.id, e.currentTarget.files?.[0]), (e.currentTarget.value = ''))} />
            </label>
            <button type="button" class="size-9 cursor-pointer text-muted" aria-label="Delete {r.values.name}" onclick={() => remove(r.id)}><Icon name="x" size={16} /></button>
          </div>
          <div class="flex flex-wrap items-center gap-1.5 text-[12px] text-muted">
            {#if r.values.category}<span>{categoryLabel(r.values.category)}</span>{/if}
            {#each r.ai.filter((k) => r.values[k] && (k === 'brand' || k === 'model')) as k (k)}<span class="font-semibold text-ink">{r.values[k]}</span>{/each}
            {#each Object.entries(r.suggestions).filter(([k]) => ['category', 'brand', 'model'].includes(k)) as [k, v] (k)}
              <button type="button" class={chip} onclick={() => s.accept(r.id, [k])}>{k === 'category' ? categoryLabel(v) : v}</button>
            {/each}
            {#if Object.keys(r.suggestions).length > 1}<button type="button" class="cursor-pointer font-semibold text-accent" onclick={() => s.accept(r.id)}>Accept all</button>{/if}
            {#if r.state === 'failed'}<span class="text-danger" role="alert">{r.error}</span>{/if}
          </div>
          {#if r.created}
            <DupHint
              brand={r.values.brand}
              model={r.values.model}
              name={r.values.name}
              excludeId={r.id}
              onresolve={(kind, m) => s.resolveDuplicate(r.id, kind, m.id)}
            />
          {/if}
        </li>
      {:else}
        <li class="px-4 py-6 text-[14px] text-muted">Type a name below and press Add.{ai.status('text') === 'working' ? ' The AI tidies each line.' : ''}</li>
      {/each}
    </ul>
  {:else if error}
    <EmptyState tone="error" title="Quick add could not start" message={error} onretry={() => ((error = ''), attempt++)} />
  {:else}
    <Skeleton rows={6} />
  {/if}
  <PackingBar />

  {#snippet bar()}
    <form class="flex w-full items-center gap-2" onsubmit={add}>
      <Button type="button" onclick={() => (describing = !describing)} aria-pressed={describing}>Describe</Button>
      <input
        bind:this={input}
        bind:value={name}
        class="h-11 min-w-0 flex-1 rounded-control border border-border-input bg-surface px-3 text-[16px] text-ink"
        placeholder="3 wool scarves"
        aria-label="Name of the next thing"
        enterkeyhint="done"
        autocomplete="off"
        disabled={!sheet}
      />
      <Button variant="primary" type="submit" disabled={!sheet || !name.trim()}>Add</Button>
    </form>
  {/snippet}
</FlowFrame>

{#if sheet}
  <PlaceDialog bind:open={placeOpen} title="Place for new lines" value={sheet.places[sheet.defaults.place] ?? null} allowNone onpick={pick} />
{/if}

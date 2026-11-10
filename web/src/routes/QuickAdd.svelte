<script lang="ts">
  import { untrack } from 'svelte'
  import { MediaQuery } from 'svelte/reactivity'
  import AiHint from '../lib/AiHint.svelte'
  import { activeHousehold } from '../lib/auth.svelte'
  import { ai } from '../lib/ai.svelte'
  import { explain } from '../lib/errors'
  import Icon from '../lib/Icon.svelte'
  import { getCatalog, categoryLabel, valueLabel } from '../lib/inventory/categories'
  import PlaceDialog from '../lib/inventory/PlaceDialog.svelte'
  import { getTree, listHomes, recentPlaces, rememberPlace } from '../lib/inventory/places'
  import PackingBar from '../lib/moving/PackingBar.svelte'
  import { boxPlace, packing } from '../lib/moving/packing.svelte'
  import DescribeBox from '../lib/quickadd/DescribeBox.svelte'
  import { Describe } from '../lib/quickadd/textrows.svelte'
  import DupHint from '../lib/quickadd/DupHint.svelte'
  import ImportDialog from '../lib/quickadd/ImportDialog.svelte'
  import Grid from '../lib/quickadd/Grid.svelte'
  import { Sheet } from '../lib/quickadd/sheet.svelte'
  import { createSuggest } from '../lib/quickadd/suggest'
  import FlowFrame from '../lib/shell/FlowFrame.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import PanelFooter from '../lib/shell/PanelFooter.svelte'
  import Toolbar from '../lib/shell/Toolbar.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import Skeleton from '../lib/ui/Skeleton.svelte'
  import { showToast } from '../lib/ui/toasts.svelte'

  const PHONE_PATH = '/quick-add/phone'

  const desktop = new MediaQuery('min-width: 768px')

  let sheet = $state.raw<Sheet>()
  let error = $state('')
  let attempt = $state(0)
  let placeOpen = $state(false)
  let importOpen = $state(false)
  const describe = new Describe()

  // The sheet only exists on a desktop-sized screen.
  $effect(() => {
    if (!desktop.current || sheet) return
    void attempt
    void ai.ensure() // suggest() asks ai.status('text') on every row
    getCatalog().then(
      (catalog) => {
        const s = new Sheet(catalog, { suggest: createSuggest(), sendAiFields: true })
        // Start where the person last put something (J7.6); a packing box overrides it below.
        const last = recentPlaces(activeHousehold()?.id ?? '')[0]
        if (last) s.setPlaceDefault(last)
        sheet = s
        loadPlaces(sheet)
      },
      (e) => (error = explain(e).message),
    )
  })

  // With a "packing now" box set, new rows go into it.
  $effect(() => {
    if (!packing.loaded) packing.load().catch(() => {})
  })
  $effect(() => {
    const s = sheet
    const b = packing.box
    if (!s || !b) return
    untrack(() => s.setPlaceDefault(boxPlace(b)))
  })

  async function loadPlaces(s: Sheet) {
    try {
      const homes = await listHomes()
      for (const h of homes.data) s.setPlaces((await getTree(h.id)).places)
    } catch {
      // Picking still works; only pasting place names needs the list.
    }
  }

  // Whatever left its row is sent before the screen goes away.
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

  function remove(index: number) {
    if (!sheet?.deleteRow(index)) return
    const s = sheet
    showToast('Row deleted', 'info', 4000, { label: 'Undo', run: () => s.undo() })
  }

  const field = 'h-7 rounded-control border border-border-input bg-surface px-2 text-[13px] text-ink'
</script>

{#if !desktop.current}
  <FlowFrame icon="table" title="Quick add">
    <div class="flex flex-col items-start gap-3 bg-surface px-4 py-6">
      <p class="m-0 text-[15px] text-body">The quick-add sheet is built for a keyboard and a wide screen. On your phone, add things from a simple list instead.</p>
      <Button variant="primary" href={PHONE_PATH} icon="arrow-right">Open phone quick add</Button>
    </div>
    <PackingBar />
  </FlowFrame>
{:else}
  <Panel>
    <PageHeader icon="table" title="Quick add" breadcrumb={['Tools']}>
      {#snippet actions()}
        <Button icon="upload" onclick={() => (importOpen = true)}>Import CSV</Button>
        <Button icon="refresh" onclick={() => sheet?.undo()} disabled={!sheet || sheet.undoDepth === 0}>Undo <kbd class="font-mono text-[11px] text-muted">⌘Z</kbd></Button>
      {/snippet}
    </PageHeader>

    {#if sheet}
      <Toolbar>
        <span class="text-[12px] font-semibold tracking-[0.06em] text-label uppercase">New rows</span>
        <label class="flex items-center gap-1.5 text-[13px] text-body">
          Category
          <select class={field} value={sheet.category} onchange={(e) => sheet!.setCategory(e.currentTarget.value)}>
            {#each sheet.catalog.categories as c (c.key)}<option value={c.key}>{categoryLabel(c.key)}</option>{/each}
          </select>
        </label>
        <span class="flex items-center gap-1.5 text-[13px] text-body">
          Place
          <button type="button" class="{field} flex max-w-60 cursor-pointer items-center gap-1.5" onclick={() => (placeOpen = true)}>
            <Icon name="pin" size={14} class="text-label" />
            <span class="truncate">{sheet.places[sheet.defaults.place]?.name ?? 'Not placed'}</span>
          </button>
        </span>
        <label class="flex items-center gap-1.5 text-[13px] text-body">
          Status
          <select class={field} value={sheet.defaults.status} onchange={(e) => sheet!.setDefaults({ status: e.currentTarget.value })}>
            {#each sheet.catalog.statuses as s (s)}<option value={s}>{valueLabel(s)}</option>{/each}
          </select>
        </label>
        <span class="ml-auto"><AiHint role="text" /></span>
        <span class="hidden text-[12px] text-muted lg:block">Type to fill a cell. <kbd class="font-mono">Tab</kbd> next, <kbd class="font-mono">↵</kbd> down, <kbd class="font-mono">⌘D</kbd> fill down, paste a list.</span>
      </Toolbar>

      <DescribeBox {describe} {sheet} />
      <div class="min-h-0 flex-1">
        <Grid {sheet} ondelete={remove} />
      </div>
      {#each sheet.rows.filter((r) => r.created) as r (r.id)}
        <DupHint brand={r.values.brand} model={r.values.model} name={r.values.name} excludeId={r.id} onresolve={(kind, m) => sheet!.resolveDuplicate(r.id, kind, m.id)} />
      {/each}

      <PackingBar />
      <PanelFooter>
        <span><span class="font-mono text-ink">{sheet.added}</span> {sheet.added === 1 ? 'row' : 'rows'} added this session</span>
        {#if sheet.saving}<span>Saving {sheet.saving}</span>{/if}
        {#if sheet.failed}<span class="text-danger" role="alert">{sheet.failed} not saved, press the mark to retry</span>{/if}
        <a href="/items?draft=1" class="ml-auto inline-flex items-center gap-1 font-semibold text-accent hover:underline">Review drafts<Icon name="arrow-right" size={13} /></a>
      </PanelFooter>

      <PlaceDialog
        bind:open={placeOpen}
        title="Place for new rows"
        value={sheet.places[sheet.defaults.place] ?? null}
        allowNone
        onpick={(p) => {
          sheet!.setPlaceDefault(p)
          if (p) rememberPlace(activeHousehold()?.id ?? '', p)
        }}
      />
    {:else if error}
      <EmptyState tone="error" title="Quick add could not start" message={error} onretry={() => ((error = ''), attempt++)} />
    {:else}
      <Skeleton rows={8} />
    {/if}
  </Panel>
  <ImportDialog bind:open={importOpen} />
{/if}

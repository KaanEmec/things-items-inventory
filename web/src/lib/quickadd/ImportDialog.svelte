<script lang="ts">
  import { explain } from '../errors'
  import PlaceDialog from '../inventory/PlaceDialog.svelte'
  import type { PlaceShort } from '../inventory/types'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { dateFormats, dryRunImport, previewImport, startImport, targets, undoImport, waitForJob, type DryRun, type JobState, type Mapping, type Preview, type Started } from './csvimport'

  interface Props {
    open?: boolean
    /** Called when an import finished, so the host can refresh. */
    ondone?: () => void
  }
  let { open = $bindable(false), ondone }: Props = $props()

  type Step = 'upload' | 'map' | 'check' | 'run'
  let step = $state<Step>('upload')
  let file = $state.raw<File | null>(null)
  let preview = $state.raw<Preview | null>(null)
  let mapping = $state<Mapping | null>(null)
  let dry = $state.raw<DryRun | null>(null)
  let started = $state.raw<Started | null>(null)
  let job = $state.raw<JobState | null>(null)
  let busy = $state(false)
  let error = $state('')
  let pickFor = $state('')
  let pickOpen = $state(false)
  let names = $state<Record<string, string>>({})

  const hasName = $derived(!!mapping?.columns.some((c) => c.target === 'name'))
  const running = $derived(step === 'run' && !!job && !['done', 'failed', 'cancelled'].includes(job.state))

  function reset() {
    step = 'upload'
    file = preview = dry = started = job = null
    mapping = null
    error = ''
    names = {}
  }

  async function run<T>(f: () => Promise<T>): Promise<T | undefined> {
    busy = true
    error = ''
    try {
      return await f()
    } catch (e) {
      error = explain(e).message
    } finally {
      busy = false
    }
  }

  async function chosen(f: File | undefined) {
    if (!f) return
    file = f
    const p = await run(() => previewImport(f))
    if (!p) return
    preview = p
    mapping = structuredClone($state.snapshot(p.mapping)) as Mapping
    step = 'map'
  }

  async function check() {
    const d = await run(() => dryRunImport(file!, $state.snapshot(mapping!) as Mapping))
    if (!d) return
    dry = d
    const keep: Record<string, string> = {}
    for (const u of d.unmatched_places) if (mapping!.places?.[u.value]) keep[u.value] = mapping!.places[u.value]
    mapping!.places = keep
    step = 'check'
  }

  async function begin() {
    const m = $state.snapshot(mapping!) as Mapping
    const s = await run(() => startImport(file!, m))
    if (!s) return
    started = s
    job = { state: 'queued' }
    step = 'run'
    const last = await run(() => waitForJob(s.job_id, (j) => (job = j)))
    if (last?.state === 'done') ondone?.()
  }

  async function undo() {
    if (!started) return
    const ok = await run(() => undoImport(started!.import_id))
    if (ok !== undefined) {
      showToast('Import undone', 'info')
      ondone?.()
      open = false
    }
  }

  function setPlace(value: string, choice: string) {
    if (choice === '__pick') {
      pickFor = value
      pickOpen = true
      return
    }
    mapping!.places = { ...mapping!.places, [value]: choice }
    if (!choice) delete mapping!.places[value]
  }
  function picked(p: PlaceShort | null) {
    if (p && pickFor) {
      names[p.id] = p.name
      mapping!.places = { ...mapping!.places, [pickFor]: p.id }
    }
  }
  const choiceOf = (v: string) => mapping?.places?.[v] ?? ''

  $effect(() => {
    if (!open && !running) reset()
  })

  const sel = 'h-9 rounded-control border border-border-input bg-surface px-2 text-[14px] text-ink'
</script>

<Dialog bind:open title="Import a CSV file" size="lg">
  <div class="flex flex-col gap-3 p-5 text-[14px] text-body">
    {#if error}<p class="m-0 text-danger" role="alert">{error}</p>{/if}

    {#if step === 'upload'}
      <p class="m-0">Choose a CSV or TSV file, for example a spreadsheet or an export from another inventory app. Columns are matched for you.</p>
      <input type="file" accept=".csv,.tsv,.txt,text/csv,text/tab-separated-values" disabled={busy} onchange={(e) => chosen(e.currentTarget.files?.[0])} aria-label="CSV file" />
      {#if busy}<p class="m-0 text-muted" role="status">Reading the file…</p>{/if}
    {:else if step === 'map' && mapping && preview}
      <p class="m-0 text-muted">{preview.row_count} rows, read as {preview.encoding}. {preview.ai ? 'The AI proposed this mapping; check it.' : 'Columns whose header names a field are pre-selected.'}</p>
      <label class="flex items-center gap-2"><input type="checkbox" bind:checked={mapping.has_header} /> First row is a header</label>
      <div class="overflow-x-auto">
        <table class="w-full border-collapse text-[13px]">
          <thead>
            <tr>
              {#each mapping.columns as c, i (i)}
                <th class="min-w-36 p-1 text-left align-bottom font-normal">
                  <div class="truncate font-semibold text-ink">{preview.header[i] ?? `Column ${i + 1}`}</div>
                  <select class="{sel} w-full {c.confidence ? 'border-ai-field-border bg-ai-field-bg' : ''}" aria-label="Field for {preview.header[i] ?? `column ${i + 1}`}" bind:value={c.target}>
                    {#each targets as [v, l] (v)}<option value={v}>{l}</option>{/each}
                    {#if c.target === 'attribute'}<option value="attribute">Attribute: {c.attribute}</option>{/if}
                  </select>
                </th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each preview.rows.slice(0, 5) as r, ri (ri)}
              <tr class="border-t border-divider">
                {#each mapping.columns as _, i (i)}<td class="max-w-40 truncate p-1">{r[i] ?? ''}</td>{/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <div class="flex flex-wrap gap-4">
        <label class="flex items-center gap-1.5">Dates
          <select class={sel} bind:value={mapping.date_format}>{#each dateFormats as f (f)}<option value={f}>{f}</option>{/each}</select>
        </label>
        <label class="flex items-center gap-1.5"><input type="checkbox" bind:checked={mapping.decimal_comma} /> Decimal comma (12,50)</label>
      </div>
      {#if !hasName}<p class="m-0 text-danger" role="alert">Map one column to Name.</p>{/if}
    {:else if step === 'check' && dry && mapping}
      <p class="m-0 text-ink"><b>{dry.will_add}</b> of {dry.rows} rows will be added as drafts{dry.skipped.length ? `, ${dry.skipped.length} skipped` : ''}.</p>
      {#if dry.skipped.length}
        <details><summary class="cursor-pointer">Skipped rows</summary>
          <ul class="m-0 pl-5">{#each dry.skipped.slice(0, 50) as p (p.row)}<li>Row {p.row}: {p.reason}</li>{/each}</ul>
        </details>
      {/if}
      {#if dry.warnings.length}
        <details><summary class="cursor-pointer">{dry.warnings.length} values left out</summary>
          <ul class="m-0 pl-5">{#each dry.warnings.slice(0, 50) as p, i (i)}<li>Row {p.row}: {p.reason}</li>{/each}</ul>
        </details>
      {/if}
      {#if dry.unmatched_places.length}
        <section aria-label="Places not found">
          <p class="m-0 mb-1 font-semibold text-ink">These places are not in your list</p>
          {#each dry.unmatched_places as u (u.value)}
            <div class="flex items-center gap-2 py-1">
              <span class="min-w-0 flex-1 truncate">{u.value} <span class="text-muted">({u.rows})</span></span>
              <select class={sel} aria-label="Place for {u.value}" value={choiceOf(u.value) && choiceOf(u.value) !== 'create' ? '__chosen' : choiceOf(u.value)} onchange={(e) => setPlace(u.value, e.currentTarget.value)}>
                <option value="">No place</option>
                <option value="create">Create this place</option>
                <option value="__pick">Pick an existing place…</option>
                {#if choiceOf(u.value) && choiceOf(u.value) !== 'create'}<option value="__chosen">{names[choiceOf(u.value)] ?? 'Chosen place'}</option>{/if}
              </select>
            </div>
          {/each}
        </section>
      {/if}
      {#if dry.duplicates.length}
        <section aria-label="Possible duplicates" class="rounded-control bg-accent-soft p-3">
          <p class="m-0 mb-1 font-semibold text-ink">You already have {dry.duplicates.length === 1 ? 'something like this' : 'things like these'}</p>
          <p class="m-0 mb-1 text-[12px] text-muted">They are still imported; merge or delete them in the drafts later.</p>
          <ul class="m-0 pl-5">{#each dry.duplicates.slice(0, 20) as d (d.row)}<li>Row {d.row}, {d.name}: {d.matches[0].title}{d.matches[0].path ? ` · ${d.matches[0].path}` : ''}</li>{/each}</ul>
        </section>
      {/if}
    {:else if step === 'run'}
      {#if job && !['done', 'failed', 'cancelled'].includes(job.state)}
        <p class="m-0" role="status">Importing {started?.rows} rows in the background… you can keep working.</p>
      {:else if job?.state === 'done' && job.result}
        <p class="m-0 text-ink" role="status">Added {job.result.added} drafts{job.result.failed ? `, ${job.result.failed} failed` : ''}.</p>
        {#if job.result.errors?.length}<ul class="m-0 pl-5 text-danger">{#each job.result.errors.slice(0, 5) as e, i (i)}<li>{e}</li>{/each}</ul>{/if}
      {:else}
        <p class="m-0 text-danger" role="alert">The import did not finish{job?.last_error ? `: ${job.last_error}` : '.'}</p>
      {/if}
    {/if}
  </div>
  {#snippet actions()}
    {#if step === 'map'}
      <Button onclick={reset}>Back</Button>
      <Button variant="primary" disabled={!hasName || busy} onclick={check}>{busy ? 'Checking…' : 'Check'}</Button>
    {:else if step === 'check'}
      <Button onclick={() => (step = 'map')}>Back</Button>
      <Button variant="primary" disabled={busy || !dry?.will_add} onclick={begin}>Import {dry?.will_add ?? 0} rows</Button>
    {:else if step === 'run' && !running}
      {#if job?.state === 'done'}<Button onclick={undo} disabled={busy}>Undo import</Button>{/if}
      <Button variant="primary" onclick={() => (open = false)}>Done</Button>
    {/if}
  {/snippet}
</Dialog>

<PlaceDialog bind:open={pickOpen} title="Place for “{pickFor}”" onpick={picked} />

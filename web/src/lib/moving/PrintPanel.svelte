<script lang="ts">
  import { onMount } from 'svelte'
  import { activeHousehold } from '../auth.svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import { navigate } from '../router.svelte'
  import Button from '../ui/Button.svelte'
  import { listBoxes, type Box } from './boxes'
  import { buildPrintUrl, clampCopies, COPIES_MAX, COPIES_MIN, PAPERS, parseRange, printedMap, sheetsNeeded, type Paper } from './print'

  interface Props {
    /** Ids of the boxes selected on the Moving screen. */
    selected?: string[]
    /** Boxes already loaded by the host; the panel loads them itself when omitted. */
    boxes?: Box[]
    /** Where to open the print view; the default navigates in the app. */
    onopen?: (url: string) => void
  }
  let { selected = [], boxes: given, onopen = (u) => navigate(u) }: Props = $props()

  type Scope = 'selected' | 'range' | 'unprinted'
  let loaded = $state<Box[]>([])
  let loadError = $state('')
  let scope = $state<Scope>('unprinted')
  let rangeText = $state('')
  let copies = $state(3)
  let paper = $state<Paper>('a4')
  let mono = $state(false)
  let qr = $state(false)

  const all = $derived(given ?? loaded)
  const hh = $derived(activeHousehold())
  const printed = $derived(printedMap(hh?.id ?? ''))
  const isPrinted = (b: Box) => !!(b.label_printed_at || printed[b.id])

  onMount(async () => {
    if (given) return
    try {
      loaded = await listBoxes()
    } catch (e) {
      loadError = explain(e).message
    }
  })
  // Start on the selection when there is one.
  let touched = false
  $effect(() => {
    if (!touched && selected.length) scope = 'selected'
  })

  const parsed = $derived(parseRange(rangeText))
  const chosen = $derived.by<Box[]>(() => {
    if (scope === 'selected') return all.filter((b) => selected.includes(b.id))
    if (scope === 'range') {
      const want = new Set(parsed.numbers)
      return all.filter((b) => b.box_number !== null && want.has(b.box_number))
    }
    return all.filter((b) => !isPrinted(b))
  })
  const missing = $derived(scope === 'range' ? parsed.numbers.filter((n) => !all.some((b) => b.box_number === n)) : [])
  const sheets = $derived(sheetsNeeded(chosen.length, copies, 0, paper))
  const url = $derived(buildPrintUrl({ boxes: chosen.map((b) => b.box_number!).filter((n) => n != null), places: [], copies, paper, mono, qr }))

  const scopes: { id: Scope; label: string; hint: () => string }[] = [
    { id: 'selected', label: 'Selected boxes', hint: () => `${selected.length} selected` },
    { id: 'range', label: 'Number range', hint: () => 'like 3-7, 12' },
    { id: 'unprinted', label: 'All without a printed label', hint: () => `${all.filter((b) => !isPrinted(b)).length} boxes` },
  ]
</script>

<aside aria-label="Print labels" class="flex min-w-0 flex-col">
  <div class="flex flex-col gap-4 p-5">
    <p class="m-0 text-[13px] leading-normal text-muted">
      {copies === 3 ? 'Three per box: two sides and the top.' : `${copies} per box.`} The number is readable from a few metres away.
    </p>

    {#if loadError}<p class="m-0 text-[13px] text-danger" role="alert">{loadError}</p>{/if}

    <fieldset class="m-0 flex flex-col gap-2 border-0 p-0">
      <legend class="mb-2 p-0 text-[12px] font-semibold tracking-[0.06em] text-muted uppercase">Boxes</legend>
      {#each scopes as s (s.id)}
        <label class="flex items-center gap-2 text-[14px]">
          <input type="radio" name="print-scope" value={s.id} checked={scope === s.id} onchange={() => ((scope = s.id), (touched = true))} />
          <span>{s.label}</span>
          <span class="ml-auto text-[12px] text-muted">{s.hint()}</span>
        </label>
      {/each}
      {#if scope === 'range'}
        <input
          class="h-9 rounded-control border border-border-input px-3 text-[14px]"
          aria-label="Box numbers"
          placeholder="3-7, 12"
          inputmode="numeric"
          bind:value={rangeText}
        />
        {#if parsed.invalid.length}<p class="m-0 text-[12px] text-danger">Can't read: {parsed.invalid.join(', ')}</p>{/if}
        {#if missing.length}<p class="m-0 text-[12px] text-muted">No box yet: {missing.slice(0, 8).join(', ')}{missing.length > 8 ? '…' : ''}</p>{/if}
      {/if}
    </fieldset>

    <div class="flex flex-col gap-1.5">
      <span class="text-[13px] font-medium text-body">Labels per box</span>
      <div class="flex items-center gap-2">
        {#each Array.from({ length: COPIES_MAX - COPIES_MIN + 1 }, (_, i) => i + COPIES_MIN) as n (n)}
          <button
            type="button"
            class="h-9 w-9 rounded-control border text-[14px] font-semibold {copies === n ? 'border-accent bg-accent-soft text-accent' : 'border-border-input bg-surface text-ink'}"
            aria-pressed={copies === n}
            onclick={() => (copies = clampCopies(n))}>{n}</button
          >
        {/each}
      </div>
    </div>

    <div class="flex flex-col gap-1.5">
      <label for="print-paper" class="text-[13px] font-medium text-body">Paper</label>
      <select id="print-paper" bind:value={paper} class="h-9 rounded-control border border-border-input bg-surface px-2 text-[14px]">
        {#each Object.entries(PAPERS) as [k, p] (k)}<option value={k}>{p.label}</option>{/each}
      </select>
    </div>

    <fieldset class="m-0 flex flex-col gap-2.5 border-0 p-0">
      <legend class="mb-2 p-0 text-[12px] font-semibold tracking-[0.06em] text-muted uppercase">Options</legend>
      <label class="flex items-center gap-2 text-[14px]">
        <input type="checkbox" bind:checked={mono} />Black and white
        <span class="ml-auto text-[12px] text-muted">pattern band</span>
      </label>
      <label class="flex items-center gap-2 text-[14px]">
        <input type="checkbox" bind:checked={qr} />Small QR code
        <Icon name="link" class="ml-auto text-muted" />
      </label>
      {#if qr}
        <p class="m-0 text-[12px] leading-normal text-muted">The code opens the box in the app. It only works for people signed in to this household.</p>
      {/if}
    </fieldset>

    <p class="m-0 font-mono text-[12px] text-muted" aria-live="polite">
      {chosen.length} {chosen.length === 1 ? 'box' : 'boxes'} · {chosen.length * clampCopies(copies)} labels · {sheets} {sheets === 1 ? 'sheet' : 'sheets'} {PAPERS[paper].label}
    </p>
  </div>
  <div class="border-t border-divider bg-surface-subtle px-5 py-3">
    <Button variant="primary" icon="printer" class="h-10 w-full" disabled={!chosen.length} onclick={() => onopen(url)}>
      Open print view{chosen.length ? ` (${chosen.length})` : ''}
    </Button>
  </div>
</aside>

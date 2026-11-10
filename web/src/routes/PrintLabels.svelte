<script lang="ts">
  import { onMount } from 'svelte'
  import { activeHousehold } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import BoxLabel from '../lib/moving/BoxLabel.svelte'
  import { getPlace, listBoxes, type Box } from '../lib/moving/boxes'
  import PlaceLabel from '../lib/moving/PlaceLabel.svelte'
  import { GAP, layout, MARGIN, PAPERS, paginate, parsePrintUrl, placeLink, printSheet, rememberPrinted } from '../lib/moving/print'
  import { router } from '../lib/router.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'

  const req = $derived(parsePrintUrl(router.search))
  const household = $derived(activeHousehold())
  const origin = typeof location === 'undefined' ? '' : location.origin

  let boxes = $state<Box[]>([])
  let places = $state<Box[]>([])
  let loading = $state(true)
  let error = $state('')

  onMount(() => {
    void load()
    window.addEventListener('afterprint', done)
    document.documentElement.classList.add('printing-labels')
    return () => {
      window.removeEventListener('afterprint', done)
      document.documentElement.classList.remove('printing-labels')
    }
  })

  async function load() {
    try {
      const want = new Set(req.boxes)
      const [bs, ps] = await Promise.all([want.size ? listBoxes() : Promise.resolve([]), Promise.all(req.places.map((id) => getPlace(id).catch(() => null)))])
      boxes = bs.filter((b) => b.box_number !== null && want.has(b.box_number))
      places = ps.filter((p): p is Box => !!p)
    } catch (e) {
      error = explain(e).message
    } finally {
      loading = false
    }
  }

  function copiesOf(list: Box[], n: number): Box[] {
    const out: Box[] = []
    for (const b of list) for (let i = 0; i < n; i++) out.push(b)
    return out
  }
  // Box labels first (copies of one box stay together), then place labels; the two never share a sheet.
  const boxLayout = $derived(layout(req.paper, 'box'))
  const placeLayout = $derived(layout(req.paper, 'place'))
  const boxPages = $derived(paginate(copiesOf(boxes, req.copies), boxLayout.perSheet))
  const placePages = $derived(paginate(places, placeLayout.perSheet))
  const sheets = $derived(boxPages.length + placePages.length)
  const paper = $derived(PAPERS[req.paper])
  const missing = $derived(req.boxes.filter((n) => !boxes.some((b) => b.box_number === n)))

  // The browser's print dialog was closed (printed or cancelled; it cannot tell which).
  function done() {
    if (boxes.length) rememberPrinted(household?.id ?? '', boxes.map((b) => b.id))
  }

  // @page cannot be scoped, so the paper size goes into the document head.
  $effect(() => {
    const el = document.createElement('style')
    el.textContent = `@page { size: ${paper.w}mm ${paper.h}mm; margin: ${MARGIN}mm; }`
    document.head.appendChild(el)
    return () => el.remove()
  })
</script>

<Panel class="no-print">
  <PageHeader icon="printer" title="Print labels" breadcrumb={['Moving']}>
    {#snippet actions()}
      <Button variant="primary" icon="printer" disabled={!sheets} onclick={() => window.print()}>Print</Button>
    {/snippet}
  </PageHeader>
  <Body>
    <div class="flex flex-col items-center gap-4 bg-surface-sunken p-4 md:p-6">
      <p class="m-0 max-w-[640px] text-center text-[13px] text-muted" role="status">
        {#if loading}Loading labels…
        {:else if error}<span class="text-danger" role="alert">{error}</span>
        {:else if !sheets}Nothing to print. Pick boxes in the print panel on the Moving screen.
        {:else}{sheets} {sheets === 1 ? 'sheet' : 'sheets'} of {paper.label}. Turn on "Background graphics" if the preview loses its colours.{/if}
        {#if missing.length}<br />No box with number {missing.join(', ')}.{/if}
      </p>
      <div use:printSheet class="sheets" style:--pw="{paper.w}mm" style:--ph="{paper.h}mm" style:--m="{MARGIN}mm" style:--gap="{GAP}mm">
        {#each boxPages as page, i (i)}
          <section class="page" aria-label="Sheet {i + 1}" style:--cols={boxLayout.cols} style:--cw="{boxLayout.w}mm">
            {#each page as b, j (j)}
              <div class="cell">
                <BoxLabel size="print" number={b.box_number} room={b.goes_to} handling={b.handling ?? []} household={household?.name ?? ''} mono={req.mono} qr={req.qr ? placeLink(origin, b.id) : ''} />
              </div>
            {/each}
          </section>
        {/each}
        {#each placePages as page, i (i)}
          <section class="page" aria-label="Sheet {boxPages.length + i + 1}" style:--cols={placeLayout.cols} style:--cw="{placeLayout.w}mm">
            {#each page as p (p.id)}
              <div class="cell">
                <PlaceLabel name={p.name} path={p.path} household={household?.name ?? ''} mono={req.mono} qr={req.qr ? placeLink(origin, p.id) : ''} />
              </div>
            {/each}
          </section>
        {/each}
      </div>
    </div>
  </Body>
</Panel>

<style>
  .sheets {
    display: flex;
    flex-direction: column;
    gap: 16px;
    align-items: flex-start;
    max-width: 100%;
  }
  .page {
    box-sizing: border-box;
    width: var(--pw);
    min-height: var(--ph);
    padding: var(--m);
    background: #fff;
    box-shadow: 0 1px 6px rgb(0 0 0 / 0.18);
    display: grid;
    grid-template-columns: repeat(var(--cols), var(--cw));
    justify-content: center;
    align-content: start;
    gap: var(--gap);
    print-color-adjust: exact;
    -webkit-print-color-adjust: exact;
  }
  .cell {
    outline: 0.25mm dashed #8a8a84;
    outline-offset: 2mm;
    break-inside: avoid;
  }
  .sheets {
    overflow-x: auto;
  }

  @media print {
    :global(html.printing-labels body > *:not([data-print-sheet])) {
      display: none !important;
    }
    :global(html.printing-labels),
    :global(html.printing-labels body) {
      background: #fff !important;
      height: auto !important;
      overflow: visible !important;
    }
    .sheets {
      display: block;
      overflow: visible;
    }
    .page {
      width: auto;
      min-height: 0;
      padding: 0;
      box-shadow: none;
      break-after: page;
      break-inside: avoid;
    }
    .page:last-child {
      break-after: auto;
    }
    .cell {
      outline-color: #666;
    }
  }
</style>

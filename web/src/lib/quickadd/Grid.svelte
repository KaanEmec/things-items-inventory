<script lang="ts">
  import { tick } from 'svelte'
  import Icon from '../Icon.svelte'
  import PlaceDialog from '../inventory/PlaceDialog.svelte'
  import type { Column, Row, Sheet } from './sheet.svelte'

  interface Props {
    sheet: Sheet
    /** Called when a row's delete button is pressed. */
    ondelete: (index: number) => void
  }
  let { sheet, ondelete }: Props = $props()

  const ROW_H = 36
  const HEAD_H = 36
  const GUTTER = 76
  const OVERSCAN = 8

  let scroller: HTMLDivElement
  let scrollTop = $state(0)
  let viewH = $state(600)

  const total = $derived(sheet.columns.reduce((n, c) => n + c.width, GUTTER))
  const first = $derived(Math.max(0, Math.floor(scrollTop / ROW_H) - OVERSCAN))
  const last = $derived(Math.min(sheet.rows.length, Math.ceil((scrollTop + viewH) / ROW_H) + OVERSCAN))
  const visible = $derived(sheet.rows.slice(first, last).map((row, i) => ({ row, index: first + i })))

  const placeOpen = $derived(sheet.editing !== null && sheet.columns[sheet.editing.col]?.kind === 'place')

  function ensureVisible(row: number) {
    const top = row * ROW_H
    const room = viewH - HEAD_H
    let st = scrollTop
    if (top < st) st = top
    else if (top + ROW_H > st + room) st = top + ROW_H - room
    if (st !== scrollTop) {
      scrollTop = st
      scroller.scrollTop = st
    }
  }

  // Keep the active cell in view and focused (unless focus sits in something else, like the header band).
  $effect(() => {
    const { row, col } = sheet.cursor
    const editing = sheet.editing
    if (!scroller) return
    ensureVisible(row)
    if (editing) return
    tick().then(() => {
      const a = document.activeElement
      if (a && a !== document.body && !scroller.contains(a)) return
      const el = scroller.querySelector<HTMLElement>(`[data-r="${row}"][data-c="${col}"]`)
      if (el && document.activeElement !== el) el.focus({ preventScroll: false })
    })
  })

  function onkeydown(e: KeyboardEvent) {
    if (e.isComposing) return
    const t = e.target as HTMLElement
    if (!t.closest('[role=gridcell]')) return
    if (sheet.handleKey(e)) e.preventDefault()
  }

  function onpaste(e: ClipboardEvent) {
    const text = e.clipboardData?.getData('text/plain') ?? ''
    if (sheet.editing) {
      if (!/[\n\t]/.test(text)) return // plain text goes into the open editor
      sheet.cancel()
    }
    if (sheet.paste(text)) e.preventDefault()
  }

  function cellDown(index: number, ci: number, e: MouseEvent) {
    if ((e.target as HTMLElement).closest('input,select')) return
    sheet.moveTo(index, ci, e.shiftKey)
  }

  const focusEl = (el: HTMLElement) => {
    el.focus()
    if (el instanceof HTMLInputElement) el.setSelectionRange(el.value.length, el.value.length)
  }

  const markLabel = { new: 'Not saved yet', saving: 'Saving', saved: 'Saved', failed: 'Not saved' }

  function pick(p: import('../inventory/types').PlaceShort | null) {
    const e = sheet.editing
    if (!e) return
    if (p) sheet.addPlace(p)
    sheet.cancel()
    sheet.setCell(e.row, e.col, p?.id ?? '')
  }

  const isBlank = (r: Row) => !r.values.name && !r.created
  const cellCls = (r: Row, c: Column, index: number, ci: number) => {
    const active = sheet.cursor.row === index && sheet.cursor.col === ci
    return [
      'relative box-border flex h-full flex-none items-center overflow-hidden border-r border-divider-row px-2 text-[13px] outline-none',
      c.mono ? 'font-mono text-[12px]' : '',
      active ? 'z-[1] ring-2 ring-accent ring-inset' : '',
      !active && sheet.inSelection(index, ci) ? 'bg-accent-row' : '',
      !sheet.cellApplies(r, c) ? 'bg-surface-subtle' : '',
    ].join(' ')
  }
</script>

<!-- A hand-built grid: only the rows near the viewport exist in the DOM; the cursor cell is the one tab stop. -->
<!-- svelte-ignore a11y_interactive_supports_focus (the roving tab stop is the active gridcell) -->
<div
  bind:this={scroller}
  bind:clientHeight={viewH}
  onscroll={() => (scrollTop = scroller.scrollTop)}
  {onkeydown}
  {onpaste}
  role="grid"
  aria-label="New items"
  aria-rowcount={sheet.rows.length + 1}
  aria-colcount={sheet.columns.length}
  aria-multiselectable="true"
  class="relative h-full overflow-auto bg-surface"
>
  <div style:width="{total}px" class="min-w-full">
    <div role="row" aria-rowindex="1" class="sticky top-0 z-10 flex border-b border-divider bg-surface-subtle" style:height="{HEAD_H}px">
      <div role="columnheader" aria-label="Row status" class="sticky left-0 z-10 flex-none border-r border-divider bg-surface-subtle" style:width="{GUTTER}px"></div>
      {#each sheet.columns as c (c.key)}
        <div role="columnheader" class="flex flex-none items-center border-r border-divider px-2 text-[11px] font-semibold tracking-[0.06em] text-label uppercase" style:width="{c.width}px">
          <span class="truncate">{c.label}</span>
        </div>
      {/each}
    </div>

    <div class="relative" style:height="{sheet.rows.length * ROW_H}px">
      {#each visible as { row, index } (row.id)}
        <div
          role="row"
          aria-rowindex={index + 2}
          class="group absolute left-0 flex border-b border-divider-row hover:bg-surface-subtle/60"
          style:top="{index * ROW_H}px"
          style:height="{ROW_H}px"
          style:width="{total}px"
        >
          <div role="rowheader" class="sticky left-0 z-[2] flex flex-none items-center gap-1 border-r border-divider bg-surface-subtle px-1.5" style:width="{GUTTER}px">
            <span class="w-6 text-right font-mono text-[11px] text-faint">{index + 1}</span>
            {#if isBlank(row)}
              <span class="size-5"></span>
            {:else if row.state === 'failed'}
              <button type="button" tabindex="-1" onclick={() => sheet.retry(index)} title="{row.error} Retry." aria-label="Not saved: {row.error} Retry saving" class="flex size-6 cursor-pointer items-center justify-center rounded-control text-danger hover:bg-chrome-hover">
                <Icon name="refresh" size={14} />
              </button>
            {:else}
              <span class="flex size-6 items-center justify-center {row.state === 'saved' ? 'text-use-fg' : 'text-faint'}" title={markLabel[row.state]} role="img" aria-label={markLabel[row.state]}>
                {#if row.state === 'saved'}<Icon name="check" size={14} />{:else if row.state === 'saving'}<Icon name="clock" size={14} />{:else}<span class="size-1.5 rounded-full bg-border-input"></span>{/if}
              </span>
            {/if}
            {#if !isBlank(row)}
              <button type="button" tabindex="-1" onclick={() => ondelete(index)} aria-label="Delete row {index + 1}" class="flex size-6 cursor-pointer items-center justify-center rounded-control text-faint opacity-0 group-hover:opacity-100 hover:bg-chrome-hover focus-visible:opacity-100">
                <Icon name="x" size={14} />
              </button>
            {/if}
          </div>

          {#each sheet.columns as c, ci (c.key)}
            {@const active = sheet.cursor.row === index && sheet.cursor.col === ci}
            {@const edit = sheet.editing && sheet.editing.row === index && sheet.editing.col === ci ? sheet.editing : null}
            {@const ghost = sheet.suggestionAt(index, ci)}
            {@const isAi = row.ai.includes(c.key)}
            <div
              role="gridcell"
              data-r={index}
              data-c={ci}
              tabindex={active && !edit ? 0 : -1}
              aria-selected={active || sheet.inSelection(index, ci)}
              aria-readonly={!sheet.cellApplies(row, c) || undefined}
              aria-label={ghost ? `${c.label}, suggestion ${ghost}, Tab to accept` : undefined}
              class="{cellCls(row, c, index, ci)} {isAi ? 'bg-ai-field-bg' : ''}"
              style:width="{c.width}px"
              onmousedown={(e) => cellDown(index, ci, e)}
              ondblclick={() => (sheet.moveTo(index, ci), sheet.startEdit())}
            >
              {#if edit && c.kind === 'select'}
                <select
                  {@attach focusEl}
                  bind:value={sheet.editing!.draft}
                  onfocusout={() => sheet.editing && sheet.commit()}
                  aria-label={c.label}
                  class="h-7 w-full rounded-control border border-border-input bg-surface px-1 text-[13px]"
                >
                  {#each c.options ?? [] as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
                </select>
              {:else if edit && c.kind !== 'place'}
                <input
                  {@attach focusEl}
                  bind:value={sheet.editing!.draft}
                  onfocusout={() => sheet.editing && sheet.commit()}
                  inputmode={c.kind === 'int' ? 'numeric' : c.kind === 'number' ? 'decimal' : undefined}
                  aria-label={c.label}
                  autocomplete="off"
                  class="h-full w-full min-w-0 border-0 bg-transparent p-0 text-[13px] outline-0 {c.mono ? 'font-mono text-[12px]' : ''}"
                />
              {:else if sheet.cellApplies(row, c)}
                {@const text = sheet.display(row, c)}
                {#if text}
                  <span class="truncate" title={c.kind === 'place' ? sheet.places[row.values.place]?.path : undefined}>{text}</span>
                  {#if isAi}<Icon name="sparkle" size={11} class="ml-1 flex-none text-ai-fg" />{/if}
                {:else if ghost}
                  <span class="truncate text-ai-fg italic opacity-70">{c.options?.find((o) => o.value === ghost)?.label ?? ghost}</span>
                  {#if active}<kbd class="ml-auto flex-none rounded border border-ai-field-border bg-ai-bg px-1 font-mono text-[10px] text-ai-fg">Tab</kbd>{/if}
                {/if}
              {/if}
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </div>
</div>

<PlaceDialog
  bind:open={() => placeOpen, (v) => !v && sheet.editing && sheet.cancel()}
  title="Choose a place"
  value={sheet.editing ? (sheet.places[sheet.rows[sheet.editing.row]?.values.place ?? ''] ?? null) : null}
  allowNone
  initialQuery={sheet.editing?.typed ? sheet.editing.draft : ''}
  onpick={pick}
/>

<script lang="ts">
  import Icon from '../Icon.svelte'
  import { segments, type Mark } from './marks'
  import { palette } from './state.svelte'

  let dlg: HTMLDialogElement
  let input = $state<HTMLInputElement>()
  let list = $state<HTMLElement>()
  let returnTo: HTMLElement | null = null

  $effect(() => {
    if (palette.open && !dlg.open) {
      returnTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
      dlg.showModal()
      input?.focus()
      input?.select()
    } else if (!palette.open && dlg.open) dlg.close()
  })

  function onclose() {
    palette.hide()
    // Native dialogs restore focus themselves; this covers the case where they do not.
    if (returnTo?.isConnected && document.activeElement !== returnTo && (!document.activeElement || document.activeElement === document.body)) returnTo.focus()
  }

  const rows = $derived(palette.rows)
  const at = $derived(Math.min(palette.cursor, rows.length - 1))
  const groups = $derived.by(() => {
    const out: { name: string; rows: { row: (typeof rows)[number]; i: number }[] }[] = []
    rows.forEach((row, i) => {
      let g = out[out.length - 1]
      if (!g || g.name !== row.group) out.push((g = { name: row.group, rows: [] }))
      g.rows.push({ row, i })
    })
    return out
  })

  $effect(() => {
    at
    list?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' })
  })

  function onkeydown(e: KeyboardEvent) {
    if (e.isComposing) return
    if (e.key === 'ArrowDown') (e.preventDefault(), palette.move(1))
    else if (e.key === 'ArrowUp') (e.preventDefault(), palette.move(-1))
    else if (e.key === 'Enter') (e.preventDefault(), palette.runCurrent())
  }
</script>

{#snippet hl(text: string, marks?: Mark[])}
  {#each segments(text, marks) as s}{#if s.mark}<mark class="rounded-[3px] bg-accent-soft px-px text-accent">{s.text}</mark>{:else}{s.text}{/if}{/each}
{/snippet}

<dialog
  bind:this={dlg}
  {onclose}
  onclick={(e) => e.target === dlg && dlg.close()}
  aria-label="Search"
  class="m-0 h-dvh max-h-none w-full max-w-none overflow-hidden rounded-none border-0 bg-surface p-0 text-ink backdrop:bg-ink/40 md:m-auto md:mt-[10vh] md:h-auto md:max-h-[70dvh] md:w-[min(92vw,600px)] md:rounded-[14px] md:border md:border-border md:shadow-popover"
>
  <div class="flex h-dvh flex-col md:h-auto md:max-h-[70dvh]">
    <div class="flex flex-none items-center gap-2 border-b border-divider px-3 pt-[max(env(safe-area-inset-top),0px)] md:pt-0">
      <Icon name="search" class="ml-1 text-muted" />
      <input
        bind:this={input}
        value={palette.query}
        oninput={(e) => palette.setQuery(e.currentTarget.value)}
        {onkeydown}
        type="text"
        role="combobox"
        aria-expanded="true"
        aria-controls="palette-list"
        aria-autocomplete="list"
        aria-activedescendant={rows.length ? `palette-opt-${at}` : undefined}
        aria-label="Search items, places and actions"
        placeholder="Search items, places, actions…"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        enterkeyhint="go"
        class="h-14 min-w-0 flex-1 border-0 bg-transparent text-[16px] outline-0 md:h-12"
      />
      <button type="button" onclick={() => dlg.close()} class="h-11 flex-none cursor-pointer rounded-control px-3 text-[15px] font-medium text-accent md:hidden">Cancel</button>
      <kbd class="hidden h-5 flex-none items-center rounded-[5px] border border-border bg-surface px-1.5 font-mono text-[11px] text-muted md:inline-flex">Esc</kbd>
    </div>

    <div bind:this={list} id="palette-list" role="listbox" aria-label="Results" class="min-h-0 flex-1 overflow-y-auto p-1.5 md:max-h-[56dvh]">
      {#each groups as g (g.name)}
        <div role="group" aria-label={g.name}>
          <div aria-hidden="true" class="px-2.5 pt-2 pb-1 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">{g.name}</div>
          {#each g.rows as { row, i } (row.key)}
            <div
              id="palette-opt-{i}"
              role="option"
              tabindex="-1"
              aria-selected={i === at}
              onclick={() => row.run()}
              onkeydown={() => {}}
              onmousemove={() => (palette.cursor = i)}
              class="flex min-h-11 cursor-pointer items-center gap-2.5 rounded-control px-2.5 py-1.5 aria-selected:bg-accent-row md:min-h-10 {i === at ? 'bg-accent-row' : ''}"
            >
              <Icon name={row.icon} class="flex-none text-muted" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-[14px] font-medium">{@render hl(row.label, row.labelMarks)}</span>
                {#if row.sub}<span class="block truncate text-[12px] text-muted">{@render hl(row.sub, row.subMarks)}</span>{/if}
              </span>
              {#if row.hint}<kbd class="inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px] text-muted">{row.hint}</kbd>{/if}
              {#if i === at}<kbd class="hidden h-5 items-center rounded-[5px] border border-border bg-surface px-1 font-mono text-[11px] text-muted md:inline-flex">↵</kbd>{/if}
            </div>
          {/each}
        </div>
      {/each}
      {#if !rows.length}
        <p class="m-0 px-3 py-6 text-center text-[14px] text-muted">{palette.error ? 'Search failed. Try again.' : palette.loading ? 'Searching' : 'No matches.'}</p>
      {/if}
    </div>
    <div class="sr-only" aria-live="polite">{palette.loading ? 'Searching' : `${rows.length} results`}</div>
    <div class="hidden flex-none items-center gap-3 border-t border-divider bg-surface-subtle px-3 py-2 text-[12px] text-muted md:flex">
      <span><kbd class="font-mono">↑↓</kbd> move</span><span><kbd class="font-mono">↵</kbd> open</span><span><kbd class="font-mono">Esc</kbd> close</span>
      {#if palette.loading}<span class="ml-auto">Searching</span>{/if}
    </div>
  </div>
</dialog>

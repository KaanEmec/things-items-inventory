<script lang="ts">
  import Button from '../ui/Button.svelte'
  import type { Describe } from './textrows.svelte'
  import type { Sheet } from './sheet.svelte'

  interface Props {
    describe: Describe
    sheet: Sheet
    /** Show the one-line dictation hint (phone). */
    hint?: boolean
  }
  let { describe, sheet, hint = false }: Props = $props()
</script>

<div class="flex flex-col gap-2 border-b border-divider bg-surface px-4 py-3">
  <label class="text-[12px] font-semibold tracking-[0.06em] text-label uppercase" for="describe-text">Describe or paste</label>
  <textarea
    id="describe-text"
    rows="3"
    class="w-full rounded-control border border-border-input bg-surface px-3 py-2 text-[14px] text-ink"
    placeholder="three hdmi cables, the old kindle and two pairs of ski gloves in box 12"
    bind:value={describe.text}
  ></textarea>
  {#if hint}<p class="m-0 text-[12px] text-muted">To dictate, use your phone keyboard's microphone.</p>{/if}
  <div class="flex items-center gap-3">
    <Button variant="primary" icon="sparkle" onclick={() => describe.run()} disabled={describe.busy || !describe.text.trim()}>{describe.busy ? 'Reading…' : 'Make rows'}</Button>
    {#if describe.busy && describe.total > 1}<span class="text-[12px] text-muted" role="status">Part {Math.min(describe.done + 1, describe.total)} of {describe.total}</span>{/if}
    {#if describe.error}<span class="text-[12px] text-danger" role="alert">{describe.error}</span>{/if}
  </div>

  {#if describe.rows.length}
    <div class="rounded-control border border-border-input" aria-label="Suggested rows">
      <ul class="m-0 list-none divide-y divide-divider p-0">
        {#each describe.rows as r, i (i)}
          <li class="flex items-center gap-2 px-3 py-2 text-[13px] text-body">
            <span class="font-mono text-muted">{r.values.quantity ?? 1}×</span>
            <span class="min-w-0 flex-1 truncate text-ink">{r.values.name}{r.values.brand ? ` · ${r.values.brand}` : ''}{r.place_id && sheet.places[r.place_id] ? ` → ${sheet.places[r.place_id].name}` : ''}</span>
            <button type="button" class="cursor-pointer text-muted hover:text-danger" aria-label="Skip {r.values.name}" onclick={() => describe.remove(i)}>✕</button>
          </li>
        {/each}
      </ul>
      <div class="flex items-center gap-3 border-t border-divider px-3 py-2">
        <Button variant="primary" onclick={() => describe.addAll(sheet)}>Add all ({describe.rows.length})</Button>
        <Button onclick={() => describe.clear()}>Discard</Button>
        {#if !describe.ai}<span class="text-[12px] text-muted">No AI set up: one row per line.</span>{/if}
      </div>
    </div>
  {/if}
</div>

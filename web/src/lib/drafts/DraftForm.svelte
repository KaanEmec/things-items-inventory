<script lang="ts">
  // The AI-filled form for one draft: questions on top, fields (violet when AI-filled, "check this" on
  // guesses), Ask again with a hint, Confirm. Typing is never overwritten: a field is only replaced from
  // the server while it is not being edited.
  import { ApiError } from '../api'
  import AiHint from '../AiHint.svelte'
  import { getItem, patchItem } from '../inventory/items'
  import { uploadItemPhoto } from '../inventory/photos'
  import type { Item } from '../inventory/types'
  import { searchApi, type Match } from '../search/api'
  import { stream } from '../stream.svelte'
  import DupHint from '../quickadd/DupHint.svelte'
  import { mergeInto } from '../quickadd/duplicates'
  import Button from '../ui/Button.svelte'
  import Field from '../ui/Field.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { answerFields, askAi, confirmDraft, isAiField, isGuess, queueState, readDraft, readLabel, readReceipt, stateText, type Need, type ReceiptLine } from './drafts'

  interface Props {
    id: string
    /** Called after Confirm succeeded. */
    onconfirmed?: (item: Item) => void
    /** Called with each fresh copy (the queue keeps its rows current). */
    onchange?: (item: Item) => void
    /** Called after the draft was merged into an item you already have (it no longer exists). */
    onmerged?: () => void
  }
  let { id, onconfirmed, onchange, onmerged }: Props = $props()

  const FIELDS = [
    ['name', 'Name'],
    ['category', 'Category'],
    ['brand', 'Brand'],
    ['model', 'Model'],
    ['color', 'Colour'],
  ] as const

  let item = $state.raw<Item | null>(null)
  let values = $state<Record<string, string>>({})
  let edited = $state<Record<string, true>>({})
  let hint = $state('')
  let ignored = $state<string[]>([])
  let attaching = $state<string | null>(null)
  let found = $state.raw<Match[]>([])
  async function find(q: string) {
    found = q.trim().length < 2 ? [] : (await searchApi(q.trim()).catch(() => null))?.items.filter((m) => m.id !== id) ?? []
  }
  /** Gives an existing item the price of a receipt line, and this receipt's date, seller and currency. */
  async function attach(l: ReceiptLine, m: Match) {
    if (!item) return
    const p = item.purchase
    try {
      await patchItem(m.id, {
        purchase: { date: p.date, seller: p.seller, currency: p.currency, ...(l.price != null ? { price_minor: Math.round(l.price * 100) } : {}) },
      })
      showToast(`Added the purchase details to ${m.title}`, 'success')
      ignored = [...ignored, l.text]
      attaching = null
      found = []
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not save.', 'error')
    }
  }
  let busy = $state(false)
  let error = $state('')
  let fileInput: HTMLInputElement
  let photoRole: 'label' | 'receipt' = 'label'

  function load(it: Item) {
    item = it
    for (const [k] of FIELDS) if (!edited[k]) values[k] = (it[k] as string | null) ?? ''
    onchange?.(it)
  }

  $effect(() => {
    const cur = id
    item = null
    values = {}
    edited = {}
    error = ''
    ignored = []
    getItem(cur).then(
      (it) => cur === id && load(it),
      (e) => cur === id && (error = e instanceof ApiError ? e.message : 'Could not load the draft.'),
    )
    return stream.onBurst(
      ['item', 'job', 'resync'],
      () => void getItem(cur).then((it) => cur === id && load(it), () => {}),
      (m) => m.type === 'resync' || (m.type === 'item' ? m.id === cur : m.item_id === cur),
    )
  })

  const draft = $derived(item ? readDraft(item) : null)
  const qstate = $derived(item ? queueState(item) : 'waiting')

  async function save(fields: Record<string, unknown>) {
    const it = await patchItem(id, fields)
    load(it)
  }
  async function blur(key: string) {
    if (!item || !edited[key] || values[key] === ((item[key as 'name'] as string | null) ?? '')) return
    try {
      await save({ [key]: values[key] })
      delete edited[key]
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not save.'
    }
  }
  async function answer(need: Need, option: string) {
    try {
      await save(answerFields(need, option))
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not save.', 'error')
    }
  }
  function takePhoto(need: Need) {
    photoRole = need.kind === 'label_photo' ? 'label' : 'receipt'
    fileInput.click()
  }
  async function picked() {
    const f = fileInput.files?.[0]
    fileInput.value = ''
    if (!f) return
    try {
      await uploadItemPhoto(id, f, photoRole)
      try {
        await (photoRole === 'label' ? readLabel(id) : readReceipt(id))
        showToast('Photo added. The AI will read it.')
      } catch {
        showToast('Photo added. The AI could not be asked, so fill the details by hand.')
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not upload the photo.', 'error')
    }
  }
  async function again() {
    busy = true
    try {
      await askAi(id, hint)
      hint = ''
      showToast('Asked the AI again.')
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not ask the AI.', 'error')
    } finally {
      busy = false
    }
  }
  /** J7.6: fold this draft (photos, place, last seen) into the item you already have. */
  async function merge(kind: 'same' | 'add', m: { id: string }) {
    if (!item) return
    const it = item
    await mergeInto(m.id, { id, place: it.place?.id ?? null, quantity: it.quantity, photos: it.photos.map((p) => p.sha256) }, kind)
    if (onmerged) onmerged()
    else onconfirmed?.(it)
  }
  export async function confirm() {
    if (!item || busy || !item.draft) return
    busy = true
    try {
      for (const k of Object.keys(edited)) if (values[k] !== ((item[k as 'name'] as string | null) ?? '')) await save({ [k]: values[k] })
      const it = await confirmDraft(id)
      load(it)
      showToast(`Confirmed ${it.name}`, 'success')
      onconfirmed?.(it)
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not confirm.', 'error')
    } finally {
      busy = false
    }
  }
</script>

<div data-slot="capture-draft-form" class="flex h-full flex-col">
  <input bind:this={fileInput} type="file" accept="image/*" capture="environment" class="hidden" onchange={picked} />
  {#if error}
    <p class="m-4 text-[13px] text-danger">{error}</p>
  {:else if !item || !draft}
    <p class="m-4 text-[13px] text-muted">Loading...</p>
  {:else}
    <div class="flex-1 overflow-y-auto p-4">
      <p class="m-0 mb-3 text-[12px] font-medium text-muted" data-testid="draft-state">{stateText[qstate]}</p>
      {#if qstate === 'none'}
        <p class="m-0 mb-3 text-[13px] text-muted">No AI for this draft. Fill in the details by hand.</p>
      {:else if qstate === 'reading' || qstate === 'waiting'}
        <p class="m-0 mb-3 text-[13px] text-muted">The AI is not done yet. You can fill the fields yourself; it will not overwrite them.</p>
      {/if}
      {#if qstate === 'failed'}
        <p class="m-0 mb-3 text-[13px] text-danger">{draft.error ?? 'The AI could not read this item.'}</p>
      {/if}

      {#each draft.needs as need (need.kind + (need.field ?? ''))}
        <section class="mb-4 rounded-control border border-ai-field-border bg-ai-field-bg p-3" aria-label="Question from the AI">
          <p class="m-0 mb-2 text-[13px] font-medium text-ink">{need.question}</p>
          {#if need.kind === 'choose'}
            <div class="flex flex-wrap gap-2">
              {#each need.options ?? [] as o (o)}
                <button type="button" class="h-9 rounded-full border border-ai-field-border bg-surface px-3 text-[13px] text-ink" onclick={() => answer(need, o)}>{o}</button>
              {/each}
            </div>
          {:else}
            <Button icon="camera" onclick={() => takePhoto(need)}>{need.kind === 'label_photo' ? 'Add a label photo to pin down the exact model' : 'Photo of the receipt'}</Button>
          {/if}
        </section>
      {/each}

      <div class="mb-3">
        <DupHint brand={values.brand} model={values.model} name={values.name} excludeId={id} onresolve={merge} />
      </div>

      <div class="flex flex-col gap-3">
        {#each FIELDS as [key, label] (key)}
          <div>
            <Field
              {label}
              ai={isAiField(item, key) && !edited[key]}
              bind:value={values[key]}
              oninput={() => (edited[key] = true)}
              onblur={() => blur(key)}
            />
            {#if isGuess(item, key) && !edited[key]}
              <p class="m-0 mt-1 text-[12px] font-medium text-repair-fg" data-testid="check-{key}">Check this</p>
            {/if}
          </div>
        {/each}
      </div>

      {#if draft.receipt_lines.filter((l) => !ignored.includes(l.text)).length}
        <section class="mt-4 rounded-control border border-border p-3" aria-label="Other receipt lines">
          <p class="m-0 mb-2 text-[13px] font-medium text-ink">This receipt also lists</p>
          <ul class="m-0 flex list-none flex-col gap-1 p-0">
            {#each draft.receipt_lines.filter((l) => !ignored.includes(l.text)) as l (l.text)}
              <li class="flex items-center gap-2 text-[13px]">
                <span class="min-w-0 flex-1 truncate">{l.name ?? l.text}{#if l.quantity && l.quantity > 1} ×{l.quantity}{/if}</span>
                {#if l.price != null}<span class="font-mono text-muted">{l.price}</span>{/if}
                <button type="button" class="text-[12px] font-semibold text-accent" onclick={() => ((attaching = attaching === l.text ? null : l.text), (found = []))}>Attach</button>
                <button type="button" class="text-[12px] font-semibold text-accent" onclick={() => (ignored = [...ignored, l.text])}>Ignore</button>
              </li>
              {#if attaching === l.text}
                <li class="flex flex-col gap-1">
                  <input class="h-9 rounded-control border border-border-input bg-surface px-2 text-[14px]" placeholder="Find an existing item" aria-label="Find an item" oninput={(e) => find(e.currentTarget.value)} />
                  {#each found as m (m.id)}
                    <button type="button" class="truncate rounded-control px-2 py-1 text-left text-[13px] hover:bg-chrome-hover" onclick={() => attach(l, m)}>{m.title} <span class="text-muted">{m.path}</span></button>
                  {/each}
                </li>
              {/if}
            {/each}
          </ul>
        </section>
      {/if}

      {#if qstate !== 'none'}
        <div class="mt-4 flex flex-col gap-2">
          <Field label="Hint for the AI" placeholder="It's the 2021 model" bind:value={hint} />
          <Button icon="sparkle" disabled={busy} onclick={again}>Ask the AI again</Button>
          <AiHint role="vision" />
        </div>
      {/if}
    </div>
    <div class="flex gap-2 border-t border-divider p-3">
      <Button variant="primary" class="flex-1" icon={item.draft ? undefined : 'check'} disabled={busy || !item.draft} onclick={confirm}>{item.draft ? 'Confirm' : 'Confirmed'}</Button>
    </div>
  {/if}
</div>

<script lang="ts">
  import { ApiError } from '../api'
  import Icon from '../Icon.svelte'

  interface Props {
    label: string
    /** The stored value as text ('' = empty). */
    value: string
    /** Shown instead of `value` when not editing (formatted dates, labels). */
    display?: string
    kind?: 'text' | 'int' | 'number' | 'date' | 'select' | 'textarea'
    options?: { value: string; label: string }[]
    mono?: boolean
    /** Filled by AI and not yet touched or confirmed. */
    ai?: boolean
    /** Read-only (for example multi-value attributes, which are edited in the form). */
    locked?: boolean
    /** Keys of `ApiError.fields` that belong to this field. */
    errorKeys?: string[]
    /** Saves the new text; throw to keep the field open with the error. */
    save: (value: string) => Promise<void>
  }
  let { label, value, display, kind = 'text', options = [], mono = false, ai = false, locked = false, errorKeys = [], save }: Props = $props()

  let editing = $state(false)
  let draft = $state('')
  let error = $state('')
  let busy = false
  const id = $props.id()

  function start() {
    if (locked) return
    draft = value
    error = ''
    editing = true
  }
  function cancel() {
    editing = false
    error = ''
  }
  async function commit() {
    if (busy || !editing) return
    const next = draft.trim()
    if (next === value) return cancel()
    busy = true
    try {
      await save(next)
      editing = false
      error = ''
    } catch (e) {
      if (e instanceof ApiError) {
        const key = errorKeys.find((k) => e.fields[k]) ?? Object.keys(e.fields)[0]
        error = (key && e.fields[key]) || e.message
      } else error = 'Could not save. Try again.'
    } finally {
      busy = false
    }
  }
  const focus = (el: HTMLElement) => el.focus()
  const inputCls = 'min-h-11 w-full rounded-control border bg-surface px-2.5 text-[16px] md:min-h-9 md:text-[14px] ' // text size 16 stops iOS zoom
  const shown = $derived(display ?? value)
</script>

<div class="flex flex-wrap items-start gap-x-3 px-4 py-1 md:px-6 {ai ? 'bg-ai-field-bg' : ''}">
  <label for={id} class="flex min-h-11 w-24 flex-none items-center gap-1 text-[13px] text-muted md:min-h-9 md:w-28">
    {label}
    {#if ai}<span class="inline-flex h-[16px] items-center gap-[2px] rounded-full bg-ai-bg px-1 text-[9px] font-bold text-ai-fg"><Icon name="sparkle" size={9} />AI</span>{/if}
  </label>
  <div class="min-w-0 flex-1">
    {#if editing}
      {#if kind === 'select'}
        <select {id} bind:value={draft} onchange={commit} onblur={commit} onkeydown={(e) => e.key === 'Escape' && cancel()} {@attach focus} class="{inputCls} {error ? 'border-danger' : 'border-border-input'}" aria-invalid={error ? true : undefined}>
          {#each options as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
        </select>
      {:else if kind === 'textarea'}
        <textarea {id} bind:value={draft} onblur={commit} onkeydown={(e) => e.key === 'Escape' && cancel()} {@attach focus} rows="3" class="{inputCls} py-2 {error ? 'border-danger' : 'border-border-input'}" aria-invalid={error ? true : undefined}></textarea>
      {:else}
        <input
          {id}
          bind:value={draft}
          type={kind === 'date' ? 'date' : 'text'}
          inputmode={kind === 'int' ? 'numeric' : kind === 'number' ? 'decimal' : undefined}
          onblur={commit}
          onkeydown={(e) => (e.key === 'Enter' ? e.currentTarget.blur() : e.key === 'Escape' && cancel())}
          {@attach focus}
          class="{inputCls} {mono ? 'font-mono' : ''} {error ? 'border-danger' : 'border-border-input'}"
          aria-invalid={error ? true : undefined}
        />
      {/if}
    {:else}
      <button
        type="button"
        {id}
        onclick={start}
        disabled={locked}
        class="min-h-11 w-full rounded-control px-2.5 text-left text-[15px] md:min-h-9 md:text-[14px] {locked ? 'cursor-default' : 'cursor-text hover:bg-chrome-hover'} {mono ? 'font-mono text-[13px]' : ''} {shown ? '' : 'text-faint'}"
      >
        {shown || 'Add'}
      </button>
    {/if}
    {#if error}<p role="alert" class="m-0 px-2.5 pb-1 text-[12px] text-danger">{error}</p>{/if}
  </div>
</div>

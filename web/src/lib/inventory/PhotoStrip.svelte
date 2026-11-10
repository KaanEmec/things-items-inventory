<script lang="ts">
  import Icon from '../Icon.svelte'
  import CategoryTile from '../ui/CategoryTile.svelte'
  import { photoUrl } from './photos'
  import type { ItemPhoto } from './types'

  interface Props {
    photos: ItemPhoto[]
    category: string
    canEdit?: boolean
    busy?: boolean
    /** Files picked with the file input (re-encoding and upload are the caller's job). */
    onadd: (files: File[]) => void
    /** Moves an item photo one step earlier (-1) or later (+1). */
    onmove: (photo: ItemPhoto, step: -1 | 1) => void
    onmain: (photo: ItemPhoto) => void
    ondelete: (photo: ItemPhoto) => void
    class?: string
  }
  let { photos, category, canEdit = false, busy = false, onadd, onmove, onmain, ondelete, class: cls = '' }: Props = $props()

  // Item photos first (they are the gallery, the first one is the main photo), then label and receipt photos.
  const ordered = $derived([...photos.filter((p) => p.role === 'item'), ...photos.filter((p) => p.role !== 'item')])
  const gallery = $derived(ordered.filter((p) => p.role === 'item').length)
  let sel = $state(0)
  const at = $derived(Math.min(sel, ordered.length - 1))
  const cur = $derived(ordered[at])
  const isGallery = $derived(!!cur && cur.role === 'item')

  let input = $state<HTMLInputElement>()
  let full = $state<HTMLDialogElement>()
  const btn = 'inline-flex h-11 cursor-pointer items-center gap-1.5 rounded-control border border-border-input bg-surface px-3 text-[13px] font-semibold shadow-button hover:bg-surface-subtle disabled:cursor-not-allowed disabled:opacity-40 md:h-8'
</script>

<div class="border-b border-divider {cls}">
  <div class="relative flex h-[210px] items-center justify-center bg-gadget-bg text-gadget-fg md:h-[170px]">
    {#if cur}
      <button type="button" onclick={() => full?.showModal()} aria-label="View photo full size" class="size-full cursor-zoom-in">
        <img src={photoUrl(cur.sha256, 'screen')} alt="" class="size-full object-contain" />
      </button>
      <span class="absolute bottom-3 left-4 inline-flex h-6 items-center gap-1.5 rounded-full bg-white/85 px-2.5 font-mono text-[11px]">
        <Icon name="image" size={12} />{at + 1} / {ordered.length}{#if cur.role !== 'item'}<span class="font-sans font-semibold capitalize"> · {cur.role}</span>{/if}
      </span>
      {#if at > 0}
        <button type="button" onclick={() => (sel = at - 1)} aria-label="Previous photo" class="absolute left-2 flex size-11 cursor-pointer items-center justify-center rounded-full bg-white/85 shadow-button"><Icon name="chevron-left" size={18} /></button>
      {/if}
      {#if at < ordered.length - 1}
        <button type="button" onclick={() => (sel = at + 1)} aria-label="Next photo" class="absolute right-2 flex size-11 cursor-pointer items-center justify-center rounded-full bg-white/85 shadow-button"><Icon name="chevron-right" size={18} /></button>
      {/if}
    {:else}
      <CategoryTile {category} size={64} />
      <span class="absolute bottom-3 left-4 text-[12px] text-muted">No photos yet</span>
    {/if}
  </div>

  {#if ordered.length > 1}
    <div class="flex gap-2 overflow-x-auto px-4 pt-3 md:px-6">
      {#each ordered as p, i (p.link_id)}
        <button type="button" onclick={() => (sel = i)} aria-label="Show photo {i + 1}" aria-current={i === at ? 'true' : undefined} class="size-11 flex-none cursor-pointer overflow-hidden rounded-control border-2 md:size-12 {i === at ? 'border-accent' : 'border-transparent'}">
          <img src={photoUrl(p.sha256, 'thumb')} alt="" class="size-full object-cover" loading="lazy" />
        </button>
      {/each}
    </div>
  {/if}

  {#if canEdit}
    <div class="flex flex-wrap gap-2 px-4 py-3 md:px-6">
      <button type="button" class={btn} disabled={busy} onclick={() => input?.click()}><Icon name="plus" />{busy ? 'Adding' : 'Add photo'}</button>
      {#if cur}
        {#if isGallery}
          <button type="button" class={btn} disabled={busy || at === 0} onclick={() => onmain(cur)}><Icon name="check" />Main</button>
          <button type="button" class={btn} disabled={busy || at === 0} onclick={() => (onmove(cur, -1), (sel = at - 1))} aria-label="Move earlier"><Icon name="chevron-left" /></button>
          <button type="button" class={btn} disabled={busy || at >= gallery - 1} onclick={() => (onmove(cur, 1), (sel = at + 1))} aria-label="Move later"><Icon name="chevron-right" /></button>
        {/if}
        <button type="button" class="{btn} text-danger" disabled={busy} onclick={() => ondelete(cur)}><Icon name="trash" />Delete</button>
      {/if}
    </div>
    <input
      bind:this={input}
      type="file"
      accept="image/*,.heic,.heif"
      multiple
      hidden
      onchange={(e) => {
        const files = [...(e.currentTarget.files ?? [])]
        e.currentTarget.value = ''
        if (files.length) onadd(files)
      }}
    />
  {/if}
</div>

<dialog bind:this={full} onclick={() => full?.close()} aria-label="Photo" class="m-auto max-h-[96dvh] max-w-[96vw] border-0 bg-transparent p-0 backdrop:bg-black/80">
  {#if cur}<img src={photoUrl(cur.sha256, 'orig')} alt="" class="max-h-[96dvh] max-w-[96vw] object-contain" />{/if}
</dialog>

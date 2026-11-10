<script lang="ts">
  import Icon from '../Icon.svelte'
  import { photoUrl } from '../inventory/photos'
  import type { TreePlace } from '../inventory/types'
  import Button from '../ui/Button.svelte'
  import Popover from '../ui/Popover.svelte'
  import { kindIcon, roomColor } from './kinds'

  interface Props {
    place: TreePlace
    photo: string | null
    photoBusy?: boolean
    canWrite: boolean
    phone?: boolean
    onphoto: (file: File) => void
    onadd: () => void
    onrename: () => void
    onmove: () => void
    ongoesto: () => void
    ondelete: () => void
  }
  let { place, photo, photoBusy = false, canWrite, phone = false, onphoto, onadd, onrename, onmove, ongoesto, ondelete }: Props = $props()

  let input = $state<HTMLInputElement>()
  let more = $state(false)
  const trail = $derived(place.path.split(' · ').slice(1).reverse())
  const c = $derived(place.counts ?? { items: 0, items_below: 0, lines: 0 })
  const menu = 'flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-left text-[14px] hover:bg-chrome-hover md:h-9'
</script>

<div class="flex flex-wrap items-center gap-4 border-b border-divider bg-surface px-4 py-4 md:px-6 {phone ? 'flex-col items-stretch' : ''}">
  <div class="relative flex-none overflow-hidden rounded-[12px] border border-border bg-stored-bg text-stored-fg {phone ? 'h-[190px] w-full' : 'h-[96px] w-[128px]'}">
    {#if photo}
      <img src={photoUrl(photo, 'screen')} alt="Photo of {place.name}" class="size-full object-cover" />
    {:else}
      <div class="flex size-full items-center justify-center"><Icon name={kindIcon[place.kind] ?? 'pin'} size={phone ? 34 : 28} /></div>
    {/if}
    {#if canWrite}
      <button
        type="button"
        onclick={() => input?.click()}
        disabled={photoBusy}
        aria-label={photo ? 'Replace photo' : 'Add photo'}
        class="absolute right-2 bottom-2 inline-flex h-11 cursor-pointer items-center gap-1.5 rounded-control bg-white/90 px-2.5 text-[12px] font-semibold shadow-button hover:bg-white disabled:opacity-60 md:h-8"
      >
        <Icon name="camera" size={14} />{photoBusy ? 'Uploading' : photo ? 'Replace' : 'Add photo'}
      </button>
      <input bind:this={input} type="file" accept="image/*" class="hidden" onchange={(e) => { const f = e.currentTarget.files?.[0]; e.currentTarget.value = ''; if (f) onphoto(f) }} />
    {/if}
  </div>

  <div class="min-w-0 flex-[1_1_240px]">
    {#if trail.length}
      <nav aria-label="Path" class="flex flex-wrap items-center gap-1 text-[12px] text-muted">
        {#each trail as part, i}{#if i}<Icon name="chevron-right" size={11} />{/if}{part}{/each}
      </nav>
    {/if}
    <h2 class="m-0 flex items-center gap-2 text-[22px] font-bold tracking-[-0.015em]">
      {#if place.kind === 'room' && place.color && roomColor[place.color]}<span class="size-3 flex-none rounded-full" style:background={roomColor[place.color]}></span>{/if}
      <span class="truncate">{place.name}</span>
    </h2>
    <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[13px] text-body">
      <span class="font-mono"><b>{c.items}</b> here</span>
      {#if c.items_below !== c.items}<span class="font-mono"><b>{c.items_below}</b> with everything inside</span>{/if}
      {#if c.lines}<span class="font-mono"><b>{c.lines}</b> loose {c.lines === 1 ? 'line' : 'lines'}</span>{/if}
      <button
        type="button"
        disabled={!canWrite}
        onclick={ongoesto}
        class="inline-flex h-7 cursor-pointer items-center gap-1.5 rounded-full border border-border-input bg-surface px-2.5 text-[12px] font-semibold hover:bg-surface-subtle disabled:cursor-default"
        title="The place in the new home this goes to"
      >
        <Icon name="truck" size={13} class="text-muted" />
        {#if place.goes_to}
          Goes to {place.goes_to.name}{#if place.goes_to.color && roomColor[place.goes_to.color]}<span class="size-2 rounded-full" style:background={roomColor[place.goes_to.color]}></span>{/if}
        {:else}
          {canWrite ? 'Set where it goes' : 'No destination'}
        {/if}
      </button>
    </div>
  </div>

  {#if canWrite && !phone}
    <div class="flex items-center gap-2">
      <Button variant="primary" icon="plus" onclick={onadd}>Add spots</Button>
      <Popover bind:open={more} align="right">
        {#snippet trigger({ toggle, open })}
          <Button icon="more" onclick={toggle} aria-expanded={open} aria-label="More" class="w-9 px-0" />
        {/snippet}
        <button type="button" class={menu} onclick={() => ((more = false), onrename())}><Icon name="edit" class="text-muted" />Rename</button>
        <button type="button" class={menu} onclick={() => ((more = false), onmove())}><Icon name="move" class="text-muted" />Move to…</button>
        <button type="button" class={menu} onclick={() => ((more = false), ongoesto())}><Icon name="truck" class="text-muted" />Where it goes…</button>
        <button type="button" class="{menu} text-danger" onclick={() => ((more = false), ondelete())}><Icon name="trash" />Delete</button>
      </Popover>
    </div>
  {/if}
</div>

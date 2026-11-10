<script lang="ts">
  import { router } from '../router.svelte'
  import Icon from '../Icon.svelte'
  import Avatar from '../ui/Avatar.svelte'
  import CategoryTile from '../ui/CategoryTile.svelte'
  import StatusPill, { statusMeta } from '../ui/StatusPill.svelte'
  import { photoUrl } from './photos'
  import type { Item } from './types'

  interface Props {
    item: Item
    /** The item shown in the detail panel. */
    open?: boolean
    /** The keyboard position (J/K). */
    cursor?: boolean
    selectMode?: boolean
    checked?: boolean
    /** Called instead of navigating while selecting. */
    ontoggle?: () => void
    /** Called instead of navigating to /items/{id} (the Places screen shows the item in place). */
    onopen?: () => void
    class?: string
  }
  let { item, open = false, cursor = false, selectMode = false, checked = false, ontoggle, onopen, class: cls = '' }: Props = $props()

  const sub = $derived([item.brand, item.model].filter(Boolean).join(' '))
  const status = $derived(statusMeta(item.status))
  const main = $derived(item.photos?.find((p) => p.role === 'item') ?? null)
  let broken = $state<string | null>(null)
</script>

<!-- Fixed height (56 px, 64 px on phone) so the list can window. The wide layout shows the place as its own
     column; in a narrow pane the place moves under the name and the status shrinks to an icon. -->
<div
  class="flex h-16 items-center gap-3 border-b border-divider-row px-4 @[520px]:h-14 @[520px]:px-6 {open ? 'bg-accent-row' : 'hover:bg-surface-subtle'} {cursor ? 'outline-2 -outline-offset-2 outline-accent/40' : ''} {cls}"
>
  {#if selectMode}
    <input
      type="checkbox"
      {checked}
      onchange={() => ontoggle?.()}
      aria-label="Select {item.name}"
      class="size-5 flex-none cursor-pointer accent-accent"
    />
  {/if}
  <a
    href="/items/{item.id}{router.search}"
    aria-current={open ? 'true' : undefined}
    onclick={(e) => {
      if (selectMode && ontoggle) {
        e.preventDefault()
        ontoggle()
      } else if (onopen && !(e.metaKey || e.ctrlKey || e.shiftKey)) {
        e.preventDefault()
        onopen()
      }
    }}
    class="flex h-full min-w-0 flex-1 items-center gap-3"
  >
    {#if main && broken !== main.sha256}
      <img src={photoUrl(main.sha256)} alt="" loading="lazy" onerror={() => (broken = main.sha256)} class="size-[42px] flex-none rounded-[9px] object-cover @[520px]:size-8" />
    {:else}
      <span class="@[520px]:hidden"><CategoryTile category={item.category} size={42} /></span>
      <span class="hidden @[520px]:inline-flex"><CategoryTile category={item.category} size={32} /></span>
    {/if}
    <span class="min-w-0 flex-[1_1_0] @[520px]:flex-[1.1_1_0]">
      <span class="flex items-center gap-1.5">
        <b class="truncate text-[15px] font-semibold @[520px]:text-[14px]">{item.name}</b>
        {#if item.quantity > 1}<span class="flex-none font-mono text-[12px] text-muted">×{item.quantity}</span>{/if}
        {#if item.draft}<span class="flex-none text-ai-fg" title="Draft: needs a look"><Icon name="sparkle" size={13} /></span>{/if}
      </span>
      <span class="block truncate font-mono text-[12px] text-muted">{sub || ' '}</span>
      <span class="flex items-center gap-1 truncate text-[13px] text-muted @[520px]:hidden">
        <Icon name="pin" size={12} />{item.place?.path ?? 'Not placed'}
      </span>
    </span>
    <span class="hidden min-w-0 flex-[1_1_0] items-center gap-1.5 text-[13px] text-body @[520px]:flex">
      <Icon name="pin" size={13} class="text-label" /><span class="truncate">{item.place?.path ?? 'Not placed'}</span>
    </span>
    <span class="hidden flex-none @[520px]:inline-flex"><StatusPill status={item.status} /></span>
    <span class="inline-flex size-[26px] flex-none items-center justify-center rounded-full {status.cls} @[520px]:hidden" role="img" aria-label={status.label}>
      <Icon name={status.icon} size={13} />
    </span>
  </a>
  <span class="hidden flex-none @[520px]:inline-flex">{#if item.updated_by}<Avatar name={item.updated_by.name} size={22} />{:else}<span class="size-[22px]"></span>{/if}</span>
</div>

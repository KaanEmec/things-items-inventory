<script lang="ts">
  // One shared empty / error state: what is missing (or what went wrong) and what to do next.
  // tone="error" announces itself to screen readers and offers Retry when onretry is given.
  // The icon and the words carry the meaning, never colour alone.
  import type { Snippet } from 'svelte'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'
  import Button from './Button.svelte'

  interface Props {
    title: string
    message?: string
    tone?: 'empty' | 'error'
    icon?: IconName
    /** Smaller, left-aligned variant for panes and lists. */
    compact?: boolean
    onretry?: () => void
    retryLabel?: string
    /** Extra actions (buttons or links). */
    children?: Snippet
  }
  let { title, message, tone = 'empty', icon, compact = false, onretry, retryLabel = 'Try again', children }: Props = $props()
  const glyph = $derived(icon ?? (tone === 'error' ? 'alert' : 'tag'))
</script>

<div
  role={tone === 'error' ? 'alert' : 'status'}
  class="flex flex-col gap-2 {compact ? 'items-start px-4 py-4 md:px-6' : 'flex-1 items-center justify-center px-6 py-12 text-center'}"
>
  <span class="flex flex-none items-center justify-center rounded-tile {compact ? 'size-8' : 'size-12'} {tone === 'error' ? 'bg-repair-bg text-repair-fg' : 'bg-accent-soft text-accent'}">
    <Icon name={glyph} size={compact ? 16 : 22} />
  </span>
  <h2 class="m-0 font-bold text-ink {compact ? 'text-[14px]' : 'text-[17px]'}">{title}</h2>
  {#if message}<p class="m-0 max-w-[44ch] text-[14px] text-muted">{message}</p>{/if}
  {#if onretry || children}
    <div class="flex flex-wrap gap-2 {compact ? '' : 'justify-center'}">
      {#if onretry}<Button icon="refresh" onclick={onretry}>{retryLabel}</Button>{/if}
      {@render children?.()}
    </div>
  {/if}
</div>

<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from '../Icon.svelte'

  interface Props {
    open?: boolean
    title: string
    children: Snippet
    actions?: Snippet
    /** `lg` for forms. */
    size?: 'md' | 'lg'
  }
  let { open = $bindable(false), title, children, actions, size = 'md' }: Props = $props()
  let el: HTMLDialogElement

  $effect(() => {
    if (open && !el.open) el.showModal()
    else if (!open && el.open) el.close()
  })
</script>

<!-- Native <dialog>: focus trap, Escape and inert background come from the browser. -->
<dialog
  bind:this={el}
  onclose={() => (open = false)}
  onclick={(e) => e.target === el && el.close()}
  aria-labelledby="dialog-title"
  class="m-auto max-h-[92dvh] {size === 'lg' ? 'w-[min(92vw,640px)]' : 'w-[min(92vw,440px)]'} overflow-y-auto rounded-[14px] border border-border bg-surface p-0 text-ink shadow-popover backdrop:bg-ink/40 max-md:mb-0 max-md:w-full max-md:max-w-none max-md:rounded-b-none max-md:rounded-t-sheet"
>
  <div class="flex items-center gap-2 border-b border-divider px-5 py-3">
    <h2 id="dialog-title" class="flex-1 text-[16px] font-bold">{title}</h2>
    <button
      type="button"
      onclick={() => el.close()}
      aria-label="Close"
      class="flex size-11 cursor-pointer items-center justify-center rounded-control text-muted hover:bg-chrome-hover md:size-9"
    >
      <Icon name="x" size={18} />
    </button>
  </div>
  <div class="px-5 py-4">{@render children()}</div>
  {#if actions}
    <div class="flex justify-end gap-2 border-t border-divider bg-surface-subtle px-5 py-3 pb-[max(env(safe-area-inset-bottom),12px)]">
      {@render actions()}
    </div>
  {/if}
</dialog>

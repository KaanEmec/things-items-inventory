<script lang="ts">
  import type { Snippet } from 'svelte'

  interface Props {
    /** Render the trigger; spread `aria-expanded={open}` and call `toggle` on click. */
    trigger: Snippet<[{ open: boolean; toggle: () => void }]>
    children: Snippet
    align?: 'left' | 'right'
    open?: boolean
  }
  let { trigger, children, align = 'left', open = $bindable(false) }: Props = $props()
  let root: HTMLElement
</script>

<svelte:window
  onclick={(e) => open && !root.contains(e.target as Node) && (open = false)}
  onkeydown={(e) => e.key === 'Escape' && (open = false)}
/>

<div class="relative inline-block" bind:this={root}>
  {@render trigger({ open, toggle: () => (open = !open) })}
  {#if open}
    <div
      class="absolute z-30 mt-1.5 min-w-48 rounded-[10px] border border-border bg-surface p-1 shadow-popover {align === 'right' ? 'right-0' : 'left-0'}"
    >
      {@render children()}
    </div>
  {/if}
</div>

<script lang="ts">
  import Icon from '../Icon.svelte'
  import { palette } from './state.svelte'

  let { placeholder = 'Search…', class: cls = '' }: { placeholder?: string; class?: string } = $props()
</script>

<!-- Looks like a field, opens the palette (full screen on phones). -->
<button
  type="button"
  onclick={() => palette.show()}
  aria-haspopup="dialog"
  class="flex h-11 w-full cursor-pointer items-center gap-2 rounded-control border border-border-input bg-surface px-2.5 text-left text-[16px] text-muted md:h-8 md:text-[13px] {cls}"
>
  <Icon name="search" />
  <span class="flex-1 truncate">{placeholder}</span>
  <kbd class="hidden h-5 items-center rounded-[5px] border border-border px-1.5 font-mono text-[11px] md:inline-flex">⌘K</kbd>
</button>

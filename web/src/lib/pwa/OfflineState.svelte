<script lang="ts">
  import { pwa } from './pwa.svelte'

  /** Shown by a screen that has nothing loaded yet while offline. */
  let { what = 'This screen' }: { what?: string } = $props()
</script>

{#if !pwa.online}
  <div class="flex flex-col items-center gap-1 p-8 text-center" role="status">
    <p class="m-0 text-[15px] font-semibold text-ink">You're offline</p>
    <p class="m-0 max-w-xs text-[13px] text-muted">{what} needs a connection. Capture still works and uploads when you're back.</p>
  </div>
{/if}

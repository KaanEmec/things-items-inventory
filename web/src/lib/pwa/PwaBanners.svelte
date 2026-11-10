<script lang="ts">
  import { APP_NAME } from '../app'
  import Button from '../ui/Button.svelte'
  import IosGuide from './IosGuide.svelte'
  import { pwa } from './pwa.svelte'

  const guide = $derived(pwa.autoIosGuide)
</script>

<!-- Transient notices, outside the app tree so no screen has to know about them.
     Phone: above the tab bar. Desktop: bottom right. -->
<div
  class="pointer-events-none fixed inset-x-3 bottom-[calc(96px+env(safe-area-inset-bottom))] z-40 flex flex-col gap-2 md:inset-x-auto md:right-4 md:bottom-4 md:w-80"
>
  {#if pwa.update !== 'idle'}
    <div class="pointer-events-auto flex items-center gap-3 rounded-control border border-border bg-surface p-3 shadow-popover" role="status">
      <span class="min-w-0 flex-1 text-[14px] font-medium text-ink">Update available</span>
      <Button variant="primary" onclick={() => pwa.applyUpdate()} disabled={pwa.update === 'applying'}>
        {pwa.update === 'applying' ? 'Reloading' : 'Reload'}
      </Button>
    </div>
  {/if}

  {#if guide}
    <div class="pointer-events-auto rounded-control border border-border bg-surface p-4 shadow-popover" role="region" aria-label="Add to Home Screen">
      <p class="m-0 mb-3 text-[14px] font-semibold text-ink">Add {APP_NAME} to your Home Screen</p>
      <IosGuide />
      <div class="mt-3 flex justify-end"><Button variant="ghost" onclick={() => pwa.dismissIosGuide()}>Got it</Button></div>
    </div>
  {/if}
</div>

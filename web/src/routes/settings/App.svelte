<script lang="ts">
  import { APP_NAME } from '../../lib/app'
  import IosGuide from '../../lib/pwa/IosGuide.svelte'
  import { pwa } from '../../lib/pwa/pwa.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Block from './Block.svelte'

  const kind = $derived(pwa.kind)
</script>

<Block title="Install" note="Open {APP_NAME} from your Home Screen or desktop, full screen, and start faster.">
  {#if kind === 'installed'}
    <p class="m-0 text-[14px] text-body">{APP_NAME} is installed on this device.</p>
  {:else if kind === 'prompt'}
    <Button variant="primary" onclick={() => pwa.install()}>Install {APP_NAME}</Button>
  {:else if kind === 'ios'}
    <IosGuide />
  {:else}
    <p class="m-0 text-[14px] text-muted">
      Installing needs a supported browser over HTTPS: Chrome or Edge on Android and desktop, or Safari on iPhone and iPad.
    </p>
  {/if}
</Block>

<Block title="Updates">
  {#if pwa.update === 'idle'}
    <p class="m-0 text-[14px] text-body">You have the latest version.</p>
  {:else}
    <div class="flex items-center gap-3">
      <span class="text-[14px] text-body">A new version is ready.</span>
      <Button variant="primary" onclick={() => pwa.applyUpdate()}>Reload</Button>
    </div>
  {/if}
</Block>

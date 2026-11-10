<script lang="ts">
  import { MediaQuery } from 'svelte/reactivity'
  import CaptureDesktop from '../lib/capture/CaptureDesktop.svelte'
  import CapturePhone from '../lib/capture/CapturePhone.svelte'

  const desktop = new MediaQuery('min-width: 768px')
</script>

{#if desktop.current}<CaptureDesktop />{:else}<CapturePhone />{/if}

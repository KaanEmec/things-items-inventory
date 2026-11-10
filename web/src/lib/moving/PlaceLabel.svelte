<script lang="ts">
  import { qrSvg } from './qr'

  interface Props {
    name: string
    /** Inside-out path, e.g. `3rd drawer · left closet · bedroom`. */
    path?: string
    household?: string
    mono?: boolean
    qr?: string
  }
  let { name, path = '', household = '', mono = false, qr = '' }: Props = $props()
  const svg = $derived(qr ? qrSvg(qr) : '')
</script>

<div class="place-label" class:mono>
  <div class="text">
    <span class="name">{name}</span>
    {#if path && path !== name}<span class="path">{path}</span>{/if}
    <span class="hh">{household}</span>
  </div>
  {#if svg}<span class="qr" aria-hidden="true">{@html svg}</span>{/if}
</div>

<style>
  .place-label {
    container-type: inline-size;
    box-sizing: border-box;
    width: 100%;
    aspect-ratio: 90 / 40;
    display: flex;
    align-items: center;
    gap: 4cqw;
    padding: 4cqw;
    background: #fff;
    color: #1a1a19;
    border: 2px solid #1a1a19;
    border-radius: 6px;
    overflow: hidden;
    print-color-adjust: exact;
    -webkit-print-color-adjust: exact;
    break-inside: avoid;
    font-family: 'Inter Variable', system-ui, sans-serif;
  }
  .text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2cqw;
  }
  .name {
    font-weight: 800;
    font-size: 9cqw;
    line-height: 1.05;
    letter-spacing: -0.02em;
  }
  .path {
    font-size: 4.4cqw;
    line-height: 1.25;
    color: #3f3f3b;
  }
  .hh {
    font-size: 3cqw;
    color: #5f5f5a;
  }
  .qr {
    flex: none;
    width: 26cqw;
    height: 26cqw;
  }
  .qr :global(svg) {
    width: 100%;
    height: 100%;
    display: block;
  }
</style>

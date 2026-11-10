<script lang="ts">
  import qrcode from 'qrcode-generator'
  import Button from '../../lib/ui/Button.svelte'
  import CopyField from '../../lib/ui/CopyField.svelte'
  import Dialog from '../../lib/ui/Dialog.svelte'

  interface Props {
    open?: boolean
    title: string
    url: string
    note: string
    /** Show a QR code to scan from another phone. */
    qr?: boolean
  }
  let { open = $bindable(false), title, url, note, qr = false }: Props = $props()

  // Markup comes from the QR library for our own URL, not from user input.
  const svg = $derived.by(() => {
    if (!qr || !url) return ''
    const q = qrcode(0, 'M')
    q.addData(url)
    q.make()
    return q.createSvgTag({ cellSize: 4, margin: 2, scalable: true })
  })
</script>

<Dialog bind:open {title}>
  <div class="flex flex-col gap-4">
    <p class="m-0 text-[14px] text-muted">{note}</p>
    <CopyField label="Link" value={url} />
    {#if svg}
      <div class="flex flex-col items-center gap-1.5">
        <div class="size-44 rounded-control border border-border bg-white p-1 [&>svg]:size-full" role="img" aria-label="QR code for the link">{@html svg}</div>
        <p class="m-0 text-[12px] text-muted">Scan with another phone.</p>
      </div>
    {/if}
  </div>
  {#snippet actions()}<Button variant="primary" onclick={() => (open = false)}>Done</Button>{/snippet}
</Dialog>

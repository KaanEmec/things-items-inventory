<script lang="ts">
  import Button from './Button.svelte'
  import { showToast } from './toasts.svelte'

  let { label, value }: { label: string; value: string } = $props()
  const id = $props.id()

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      showToast('Link copied', 'success')
    } catch {
      document.getElementById(id)?.focus()
      ;(document.getElementById(id) as HTMLInputElement | null)?.select()
      showToast('Copy failed. Select the link and copy it by hand.', 'error')
    }
  }
</script>

<div class="flex flex-col gap-1.5">
  <label for={id} class="text-[13px] font-medium text-body">{label}</label>
  <div class="flex gap-2">
    <input {id} readonly {value} onfocus={(e) => e.currentTarget.select()} class="h-11 min-w-0 flex-1 rounded-control border border-border-input bg-surface-subtle px-3 font-mono text-[13px] text-ink md:h-9" />
    <Button icon="copy" onclick={copy}>Copy</Button>
  </div>
</div>

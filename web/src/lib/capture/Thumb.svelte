<script lang="ts">
  import Icon from '../Icon.svelte'

  interface Props {
    blob: Blob | null
    alt?: string
    class?: string
    fit?: 'cover' | 'contain'
  }
  let { blob, alt = '', class: cls = '', fit = 'cover' }: Props = $props()

  let url = $state('')
  $effect(() => {
    if (!blob) {
      url = ''
      return
    }
    const u = URL.createObjectURL(blob)
    url = u
    return () => URL.revokeObjectURL(u)
  })
</script>

{#if url}
  <img src={url} {alt} class="{fit === 'cover' ? 'object-cover' : 'object-contain'} {cls}" />
{:else}
  <span class="flex items-center justify-center bg-use-bg text-use-fg {cls}"><Icon name="check" size={18} /></span>
{/if}

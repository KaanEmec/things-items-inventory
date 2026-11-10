<script lang="ts">
  import Dialog from '../ui/Dialog.svelte'
  import PlacePicker from './PlacePicker.svelte'
  import type { PlaceShort } from './types'

  interface Props {
    open?: boolean
    title?: string
    value?: PlaceShort | null
    allowNone?: boolean
    initialQuery?: string
    onpick: (place: PlaceShort | null) => void
  }
  let { open = $bindable(false), title = 'Choose a place', value = null, allowNone = false, initialQuery, onpick }: Props = $props()
</script>

<Dialog bind:open {title}>
  {#if open}
    <PlacePicker {value} {allowNone} {initialQuery} onpick={(p) => ((open = false), onpick(p))} />
  {/if}
</Dialog>

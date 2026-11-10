<script lang="ts">
  import type { PlaceKind } from '../inventory/places'
  import type { TreePlace } from '../inventory/types'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import Field from '../ui/Field.svelte'
  import FormError from '../ui/FormError.svelte'
  import Select from '../ui/Select.svelte'
  import { childKind, expandSpots } from './logic'
  import { failure } from './store.svelte'

  interface Props {
    open?: boolean
    /** Where the new places go; null = top level of the home. */
    parent: TreePlace | null
    onadd: (kind: PlaceKind, names: string[]) => Promise<void>
  }
  let { open = $bindable(false), parent, onadd }: Props = $props()

  let text = $state('')
  let kind = $state<string>('spot')
  let busy = $state(false)
  let error = $state('')
  const parsed = $derived(expandSpots(text))

  $effect(() => {
    if (open) {
      text = ''
      error = ''
      kind = childKind(parent?.kind)
    }
  })

  const kinds = [
    { value: 'floor', label: 'Floor' },
    { value: 'room', label: 'Room' },
    { value: 'furniture', label: 'Furniture' },
    { value: 'spot', label: 'Spot (drawer, shelf, bin)' },
  ]

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (!parsed.names.length || busy) return
    busy = true
    error = ''
    try {
      await onadd(kind as PlaceKind, parsed.names)
      open = false
    } catch (err) {
      error = failure(err)
    } finally {
      busy = false
    }
  }
</script>

<Dialog bind:open title={parent ? `Add inside ${parent.name}` : 'Add to this home'}>
  <form id="add-places-form" onsubmit={submit} class="flex flex-col gap-3">
    <Field label="Name, or several at once" bind:value={text} placeholder="drawer 1–4, or: left, middle, right" error={parsed.error || undefined} autocomplete="off" />
    <Select label="Kind" bind:value={kind} options={kinds} />
    {#if parsed.names.length > 1}
      <div>
        <p class="m-0 mb-1.5 text-[12px] text-muted">This adds {parsed.names.length} places:</p>
        <div class="flex flex-wrap gap-1.5">
          {#each parsed.names.slice(0, 12) as n (n)}<span class="rounded-full bg-stored-bg px-2.5 py-0.5 text-[12px] text-stored-fg">{n}</span>{/each}
          {#if parsed.names.length > 12}<span class="py-0.5 text-[12px] text-muted">and {parsed.names.length - 12} more</span>{/if}
        </div>
      </div>
    {/if}
    <FormError message={error} />
  </form>
  {#snippet actions()}
    <Button onclick={() => (open = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="add-places-form" disabled={!parsed.names.length || busy}>
      {parsed.names.length > 1 ? `Add ${parsed.names.length}` : 'Add'}
    </Button>
  {/snippet}
</Dialog>

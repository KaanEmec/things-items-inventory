<script lang="ts">
  // SLOT for J6.7: the AI draft form (name, category, attributes, Save) for the item shown on the
  // stage. J6.7 replaces the body of this component. `itemId` is the client ULID of the selected
  // item (the draft exists on the server once its first photo has uploaded); `uploaded` says so.
  interface Props {
    itemId: string | null
    uploaded: boolean
  }
  let { itemId, uploaded }: Props = $props()
</script>

<div data-slot="capture-draft-form" data-item-id={itemId ?? ''} class="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-[13px] text-muted">
  {#if !itemId}
    Select an item in the queue.
  {:else if !uploaded}
    The draft is created as soon as the first photo has uploaded.
  {:else}
    The draft details appear here.
  {/if}
</div>

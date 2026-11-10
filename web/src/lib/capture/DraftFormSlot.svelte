<script lang="ts">
  // The AI draft form for the item shown on the stage. `itemId` is the client ULID of the selected
  // item (the draft exists on the server once its first photo has uploaded); `uploaded` says so.
  import DraftForm from '../drafts/DraftForm.svelte'
  interface Props {
    itemId: string | null
    uploaded: boolean
  }
  let { itemId, uploaded }: Props = $props()
</script>

{#if itemId && uploaded}
  <DraftForm id={itemId} />
{:else}
  <div data-slot="capture-draft-form" class="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-[13px] text-muted">
    {#if !itemId}
      Select an item in the queue.
    {:else}
      The draft is created as soon as the first photo has uploaded.
    {/if}
  </div>
{/if}

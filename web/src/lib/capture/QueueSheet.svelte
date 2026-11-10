<script lang="ts">
  import Icon from '../Icon.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import { captureQueue as queue } from './queue.svelte'
  import { roleLabel, stateClass, stateLabel } from './labels'
  import Thumb from './Thumb.svelte'

  let { open = $bindable(false) }: { open?: boolean } = $props()

  const list = $derived([...queue.entries].reverse())
</script>

<Dialog bind:open title="Uploads" size="md">
  {#if !list.length}
    <p class="m-0 text-[14px] text-muted">Nothing in the queue.</p>
  {:else}
    {#if queue.pending}
      <p class="m-0 mb-3 flex items-center gap-2 text-[13px] text-muted"><Icon name="upload" />Keep the app open to finish uploading.</p>
    {/if}
    {#if queue.memoryOnly}
      <p class="m-0 mb-3 text-[13px] text-danger">Photos are held in memory only on this browser. Closing the app loses what has not uploaded.</p>
    {/if}
    <ul class="m-0 flex list-none flex-col p-0">
      {#each list as e (e.id)}
        <li class="flex items-center gap-3 border-b border-divider-row py-2 last:border-b-0">
          <Thumb blob={e.blob} class="size-11 flex-none rounded-control" />
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2 text-[14px] font-semibold">
              {roleLabel[e.role]}
              <span class="rounded-full px-2 py-0.5 text-[11px] font-semibold {stateClass[e.state]}">{stateLabel[e.state]}</span>
            </div>
            {#if e.error && e.state !== 'uploaded'}<div class="truncate text-[12px] text-muted">{e.error}</div>{/if}
          </div>
          {#if e.state === 'failed' || (e.state === 'waiting' && e.attempts > 0)}
            <Button class="h-9 px-3" onclick={() => queue.retry(e.id)}>Retry</Button>
          {:else if e.state === 'uploaded' && e.role !== 'contents'}
            <a href="/items/{e.groupId}" class="text-[13px] font-semibold">Open item</a>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
  {#snippet actions()}
    {#if queue.failed}<Button onclick={() => queue.retryAll()}>Retry all</Button>{/if}
    {#if list.some((e) => e.state === 'uploaded')}<Button variant="ghost" onclick={() => queue.clearUploaded()}>Clear finished</Button>{/if}
    <Button variant="primary" onclick={() => (open = false)}>Done</Button>
  {/snippet}
</Dialog>

<script lang="ts">
  import { untrack } from 'svelte'
  import { auth } from '../auth.svelte'
  import Icon from '../Icon.svelte'
  import PlaceDialog from '../inventory/PlaceDialog.svelte'
  import FlowFrame from '../shell/FlowFrame.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { prepare } from './input'
  import { roleLabel, stateClass, stateLabel } from './labels'
  import Picker from './Picker.svelte'
  import QueueSheet from './QueueSheet.svelte'
  import ContentsReview from '../drafts/ContentsReview.svelte'
  import DraftFormSlot from './DraftFormSlot.svelte'
  import { captureQueue as queue, type Role } from './queue.svelte'
  import PackingBar from '../moving/PackingBar.svelte'
  import { boxPlace, packing } from '../moving/packing.svelte'
  import { CaptureSession } from './session.svelte'
  import Thumb from './Thumb.svelte'

  const session = new CaptureSession(queue, auth.householdId)

  // With a "packing now" box set, everything captured goes into it.
  $effect(() => {
    if (!packing.loaded) packing.load().catch(() => {})
  })
  $effect(() => {
    const b = packing.box
    if (b) untrack(() => session.setPlace(boxPlace(b)))
  })

  let picker: Picker
  let placeOpen = $state(false)
  let sheetOpen = $state(false)
  let draftOpen = $state(false)
  let contentsOpen = $state(false)
  let busy = $state(0)
  /** The slot the next photo goes to in Item mode. */
  let active = $state<Role>('item')

  const itemRoles: Role[] = ['item', 'label', 'receipt']
  const hint: Record<string, string> = { item: 'The thing itself', label: 'Optional · model or serial label', receipt: 'Optional · proof of purchase' }
  const role = $derived<Role>(session.mode === 'contents' ? 'contents' : active)
  const shots = $derived(session.shots)
  const needPlace = $derived(session.mode === 'contents' && !session.place)
  const contentsReady = $derived(session.mode === 'contents' && !!session.place && shots.some((e) => e.role === 'contents' && e.state === 'uploaded'))
  const reviewable = $derived(session.mode === 'item' && shots.some((e) => e.role === 'item' && e.state === 'uploaded'))

  function shoot(from: 'camera' | 'library') {
    if (session.mode === 'contents' && !session.place) {
      showToast('Choose the container first.')
      placeOpen = true
      return
    }
    if (from === 'camera') picker.openCamera(role)
    else picker.openLibrary(role)
  }

  async function onfiles(files: File[], tag: string) {
    const r = tag as Role
    busy++
    try {
      const { blobs, errors } = await prepare(files)
      if (errors.length) showToast(errors[0], 'error')
      if (blobs.length) {
        await session.take(r, blobs)
        // After the item photo, the label is the usual next one.
        if (session.mode === 'item' && r === 'item' && !session.shotsOf('label').length) active = 'label'
      }
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not queue the photo.', 'error')
    } finally {
      busy--
    }
  }

  function next() {
    if (session.nextItem()) {
      active = 'item'
      showToast(session.mode === 'item' ? `Item ${session.done} saved to the queue.` : 'Container done.', 'success', 1800)
    }
  }
</script>

<FlowFrame icon="camera" title="Capture">
  <PackingBar />
  <div class="flex flex-col pb-4">
    <!-- Place: stays until changed. -->
    <button type="button" onclick={() => (placeOpen = true)} class="flex min-h-14 w-full cursor-pointer items-center gap-3 border-0 border-b border-divider bg-surface px-4 text-left">
      <Icon name="pin" size={20} class="text-label" />
      <span class="min-w-0 flex-1">
        <span class="block text-[11px] font-semibold tracking-[0.08em] text-label uppercase">{session.mode === 'contents' ? 'Container' : 'New items go to'}</span>
        <span class="block truncate text-[15px] font-semibold">{session.place ? session.place.name : 'Not placed'}</span>
        {#if session.place}<span class="block truncate text-[12px] text-muted">{session.place.path.split(' · ').slice(1).join(' · ')}</span>{/if}
      </span>
      <span class="text-[13px] font-semibold text-accent">Change</span>
    </button>

    <div class="flex gap-0.5 border-b border-divider bg-surface px-4 py-3" role="group" aria-label="Capture mode">
      {#each [['item', 'Item'], ['contents', 'Contents']] as const as [m, label]}
        <button
          type="button"
          aria-pressed={session.mode === m}
          onclick={() => session.setMode(m)}
          class="h-11 flex-1 cursor-pointer rounded-control border border-border-input bg-surface text-[14px] font-semibold text-muted aria-pressed:border-accent aria-pressed:bg-accent-soft aria-pressed:text-accent"
        >{label}</button>
      {/each}
    </div>

    {#if session.mode === 'item'}
      <div class="mt-3 border-y border-divider bg-surface" role="radiogroup" aria-label="Which photo">
        {#each itemRoles as r (r)}
          {@const n = session.shotsOf(r).length}
          <button
            type="button"
            role="radio"
            aria-checked={active === r}
            onclick={() => (active = r)}
            class="flex min-h-14 w-full cursor-pointer items-center gap-3 border-0 border-b border-divider-row bg-surface px-4 text-left last:border-b-0 aria-checked:bg-accent-row"
          >
            <span class="flex size-5 flex-none items-center justify-center rounded-full border-2 {active === r ? 'border-accent' : 'border-border-input'}">
              {#if active === r}<span class="size-2.5 rounded-full bg-accent"></span>{/if}
            </span>
            <span class="min-w-0 flex-1">
              <span class="block text-[15px] font-semibold">{roleLabel[r]}</span>
              <span class="block text-[12px] text-muted">{hint[r]}</span>
            </span>
            {#if n}<span class="flex items-center gap-1 text-[13px] font-semibold text-use-fg"><Icon name="check" />{n}</span>{/if}
          </button>
        {/each}
      </div>
    {:else}
      <p class="m-0 px-4 pt-3 text-[13px] text-muted">Photograph the open box, drawer or shelf. Take as many photos as it needs.</p>
    {/if}

    <div class="px-4 pt-3">
      {#if needPlace}<p class="m-0 mb-2 text-[13px] font-semibold text-danger" role="status">Choose the box or place first</p>{/if}
      <Button class="w-full" icon="image" disabled={needPlace} onclick={() => shoot('library')}>Choose from library</Button>
    </div>

    <!-- What was just taken. -->
    <section aria-label="Taken in this group" class="mt-3">
      <h2 class="m-0 px-4 pb-1.5 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">
        {session.mode === 'item' ? `This item${session.done ? ` · ${session.done} done` : ''}` : 'This container'}
      </h2>
      {#if shots.length || busy}
        <ul class="m-0 flex list-none gap-2 overflow-x-auto px-4 pb-1">
          {#each shots as e (e.id)}
            <li class="relative flex-none">
              <Thumb blob={e.blob} alt="{roleLabel[e.role]} photo" class="size-20 rounded-control border border-border" />
              <span class="absolute bottom-1 left-1 rounded px-1.5 text-[10px] font-semibold {stateClass[e.state]}">{roleLabel[e.role]}</span>
              {#if e.state === 'waiting' || e.state === 'failed'}
                <button
                  type="button"
                  aria-label="Remove {roleLabel[e.role]} photo"
                  onclick={() => queue.remove(e.id)}
                  class="absolute -top-2 -right-2 flex size-8 cursor-pointer items-center justify-center rounded-full border border-border bg-surface text-body shadow-button"
                ><Icon name="x" size={14} /></button>
              {:else}
                <span class="sr-only">{stateLabel[e.state]}</span>
              {/if}
            </li>
          {/each}
          {#if busy}<li class="flex size-20 flex-none items-center justify-center rounded-control border border-dashed border-border-input text-[11px] text-muted" role="status">Preparing</li>{/if}
        </ul>
      {:else}
        <p class="m-0 px-4 text-[13px] text-muted">No photos yet.</p>
      {/if}
    </section>

    {#if contentsReady}
      <Button class="mx-4 mt-4" icon="package" onclick={() => (contentsOpen = true)}>Review box contents</Button>
    {/if}
    {#if reviewable}
      <Button class="mx-4 mt-4" icon="sparkle" onclick={() => (draftOpen = true)}>Review draft</Button>
    {/if}

    <!-- Queue. -->
    <button type="button" onclick={() => (sheetOpen = true)} class="mx-4 mt-4 flex min-h-12 cursor-pointer items-center gap-3 rounded-control border border-border bg-surface px-3 text-left">
      <Icon name="upload" class="text-label" />
      <span class="min-w-0 flex-1 text-[14px] font-semibold">
        {#if queue.failed}{queue.failed} failed{:else if queue.pending}{queue.pending} to upload{:else}Uploads{/if}
        <span class="block text-[12px] font-normal text-muted">
          {#if !queue.online && queue.pending}Offline. They upload when you are back online.
          {:else if queue.pending}Keep the app open to finish uploading
          {:else if queue.failed}Some photos did not upload. Open this to retry them.
          {:else}Everything is uploaded{/if}
        </span>
      </span>
      <Icon name="chevron-right" class="text-muted" />
    </button>
  </div>

  {#snippet bar()}
    <Button class="flex-1" icon="camera" disabled={needPlace} onclick={() => shoot('camera')}>
      {session.mode === 'contents' ? 'Take photo' : `${roleLabel[active]} photo`}
    </Button>
    <Button variant="primary" class="flex-1" icon="check" disabled={!shots.length} onclick={next}>
      {session.mode === 'contents' ? 'Next container' : 'Next item'}
    </Button>
  {/snippet}
</FlowFrame>

<Picker bind:this={picker} {onfiles} />
<PlaceDialog bind:open={placeOpen} title={session.mode === 'contents' ? 'Which container?' : 'Where do new items go?'} value={session.place} allowNone={session.mode === 'item'} onpick={(p) => session.setPlace(p)} />
<QueueSheet bind:open={sheetOpen} />
<Dialog bind:open={contentsOpen} title="Box contents" size="lg">
  {#if session.place}<ContentsReview placeId={session.place.id} />{/if}
</Dialog>
<Dialog bind:open={draftOpen} title="Draft">
  <div class="-m-1 h-[70dvh]"><DraftFormSlot itemId={session.groupId} uploaded={reviewable} /></div>
</Dialog>

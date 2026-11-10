<script lang="ts">
  import { auth } from '../auth.svelte'
  import Icon from '../Icon.svelte'
  import PlaceDialog from '../inventory/PlaceDialog.svelte'
  import FlowFrame from '../shell/FlowFrame.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import DraftFormSlot from './DraftFormSlot.svelte'
  import { prepare } from './input'
  import { groupState, roleLabel, stateClass, stateLabel } from './labels'
  import Picker from './Picker.svelte'
  import { captureQueue as queue, type QueueEntry, type Role } from './queue.svelte'
  import { CaptureSession } from './session.svelte'
  import Thumb from './Thumb.svelte'

  const session = new CaptureSession(queue, auth.householdId)

  let picker: Picker
  let placeOpen = $state(false)
  let dragging = $state(false)
  /** Several photos arrived together: waiting for "one item or one per photo?". */
  let asking = $state.raw<Blob[] | null>(null)
  let askOpen = $state(false)
  let selectedGroup = $state<string | null>(null)
  let selectedPhoto = $state<string | null>(null)
  let busy = $state(0)

  const groups = $derived.by(() => {
    const m = new Map<string, QueueEntry[]>()
    for (const e of queue.entries) {
      if (e.role === 'contents') continue
      const l = m.get(e.groupId)
      if (l) l.push(e)
      else m.set(e.groupId, [e])
    }
    return [...m].map(([id, es]) => ({ id, es, state: groupState(es) })).reverse()
  })
  const group = $derived(groups.find((g) => g.id === selectedGroup) ?? null)
  const photo = $derived(group?.es.find((e) => e.id === selectedPhoto) ?? group?.es[0] ?? null)

  async function receive(files: File[]) {
    busy++
    try {
      const { blobs, errors } = await prepare(files)
      if (errors.length) showToast(errors[0], 'error')
      if (blobs.length > 1) {
        asking = blobs
        askOpen = true
      } else if (blobs.length === 1) await add(blobs, false)
    } finally {
      busy--
    }
  }

  async function add(blobs: Blob[], split: boolean) {
    askOpen = false
    asking = null
    try {
      const ids = await session.addFiles(blobs, split)
      selectedGroup = ids[0]
      selectedPhoto = null
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not queue the photos.', 'error')
    }
  }

  async function addToSelected(files: File[]) {
    if (!group) return receive(files)
    const { blobs, errors } = await prepare(files)
    if (errors.length) showToast(errors[0], 'error')
    for (const blob of blobs) await queue.add({ groupId: group.id, role: 'item', placeId: group.es[0].placeId, blob })
  }

  function ondrop(e: DragEvent) {
    e.preventDefault()
    dragging = false
    const files = [...(e.dataTransfer?.files ?? [])]
    if (files.length) void receive(files)
  }
  function onpaste(e: ClipboardEvent) {
    const t = e.target as HTMLElement | null
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return
    const files = [...(e.clipboardData?.files ?? [])]
    if (files.length) {
      e.preventDefault()
      void receive(files)
    }
  }
  const roles: Role[] = ['item', 'label', 'receipt']
</script>

<svelte:window {onpaste} />

<FlowFrame icon="camera" title="Capture" breadcrumb={['Add things']}>
  {#snippet actions()}
    <Button icon="pin" onclick={() => (placeOpen = true)}>{session.place ? session.place.name : 'Choose place'}</Button>
    <Button variant="primary" icon="image" onclick={() => picker.openLibrary('queue')}>Choose photos</Button>
  {/snippet}

  <div class="flex h-full min-h-[520px]">
    <!-- Queue pane. -->
    <section
      aria-label="Capture queue"
      ondragover={(e) => (e.preventDefault(), (dragging = true))}
      ondragleave={() => (dragging = false)}
      ondrop={ondrop}
      class="flex w-[300px] flex-none flex-col border-r border-divider {dragging ? 'bg-accent-row' : 'bg-surface'}"
    >
      <div class="flex h-[42px] flex-none items-center gap-2 border-b border-divider px-4 text-[12px] font-semibold tracking-[0.06em] text-muted uppercase">
        <Icon name="upload" size={14} />Queue
        <span class="ml-auto font-mono normal-case">{queue.pending ? `${queue.pending} to upload` : ''}</span>
      </div>
      <div class="m-3 flex flex-col items-center gap-1 rounded-control border border-dashed {dragging ? 'border-accent text-accent' : 'border-border-input text-muted'} px-3 py-4 text-center text-[13px]">
        <Icon name="image" size={20} />
        Drop, paste or choose photos
        {#if busy}<span role="status" class="text-[12px]">Preparing</span>{/if}
      </div>
      {#if queue.pending}
        <p class="m-0 px-4 pb-2 text-[12px] text-muted">Keep the app open to finish uploading.</p>
      {/if}
      {#if queue.memoryOnly}
        <p class="m-0 px-4 pb-2 text-[12px] text-danger">Photos are not stored on this device; closing the app loses what has not uploaded.</p>
      {/if}
      <ul class="m-0 min-h-0 flex-1 list-none overflow-y-auto p-0">
        {#each groups as g (g.id)}
          {@const first = g.es[0]}
          <li class="border-t border-divider-row">
            <button
              type="button"
              onclick={() => ((selectedGroup = g.id), (selectedPhoto = null))}
              aria-current={selectedGroup === g.id}
              class="flex w-full cursor-pointer items-center gap-3 border-0 bg-transparent px-4 py-2.5 text-left hover:bg-chrome-hover aria-[current=true]:bg-accent-row"
            >
              <Thumb blob={first.blob} class="size-12 flex-none rounded-control" />
              <span class="min-w-0 flex-1">
                <span class="block text-[14px] font-semibold">{g.es.length === 1 ? '1 photo' : `${g.es.length} photos`}</span>
                <span class="mt-0.5 inline-block rounded-full px-2 py-0.5 text-[11px] font-semibold {stateClass[g.state]}">{stateLabel[g.state]}</span>
              </span>
            </button>
          </li>
        {:else}
          <li class="px-4 py-3 text-[13px] text-muted">Nothing captured yet.</li>
        {/each}
      </ul>
    </section>

    <!-- Photo stage on the dotted mat. -->
    <section
      aria-label="Photo stage"
      class="flex min-w-0 flex-1 flex-col"
      style="background-color:var(--color-surface-subtle);background-image:radial-gradient(circle,#d9d8d0 1px,transparent 1px);background-size:16px 16px"
    >
      {#if group && photo}
        <div class="flex min-h-0 flex-1 items-center justify-center p-6">
          {#if photo.blob}
            <Thumb blob={photo.blob} fit="contain" alt="{roleLabel[photo.role]} photo" class="max-h-full max-w-full rounded-control border border-border bg-surface shadow-panel" />
          {:else}
            <p class="m-0 flex items-center gap-2 text-[14px] text-use-fg"><Icon name="check" />Uploaded</p>
          {/if}
        </div>
        <div class="flex flex-none flex-wrap items-center gap-3 border-t border-divider bg-surface px-4 py-3">
          <ul class="m-0 flex list-none gap-2 p-0">
            {#each group.es as e (e.id)}
              <li>
                <button type="button" aria-label="{roleLabel[e.role]} photo" aria-current={photo.id === e.id} onclick={() => (selectedPhoto = e.id)} class="cursor-pointer rounded-control border-2 border-transparent bg-transparent p-0 aria-[current=true]:border-accent">
                  <Thumb blob={e.blob} class="size-12 rounded-[6px]" />
                </button>
              </li>
            {/each}
          </ul>
          <label class="flex items-center gap-2 text-[13px] text-muted">
            Photo of
            <select
              value={photo.role}
              disabled={photo.state === 'uploading' || photo.state === 'uploaded'}
              onchange={(ev) => queue.setRole(photo.id, ev.currentTarget.value as Role)}
              class="h-9 rounded-control border border-border-input bg-surface px-2 text-[13px] text-ink"
            >
              {#each roles as r}<option value={r}>{roleLabel[r]}</option>{/each}
            </select>
          </label>
          <span class="rounded-full px-2 py-0.5 text-[11px] font-semibold {stateClass[photo.state]}">{stateLabel[photo.state]}</span>
          {#if photo.error && photo.state !== 'uploaded'}<span class="text-[12px] text-muted">{photo.error}</span>{/if}
          <span class="ml-auto flex gap-2">
            <Button icon="plus" onclick={() => picker.openLibrary('add')}>Add photo</Button>
            {#if photo.state === 'failed' || (photo.state === 'waiting' && photo.attempts > 0)}<Button onclick={() => queue.retry(photo.id)}>Retry</Button>{/if}
            {#if photo.state === 'waiting' || photo.state === 'failed'}<Button icon="trash" onclick={() => queue.remove(photo.id)}>Remove</Button>{/if}
            {#if group.state === 'uploaded'}<Button href="/items/{group.id}" variant="primary">Open item</Button>{/if}
          </span>
        </div>
      {:else}
        <div class="flex flex-1 flex-col items-center justify-center gap-2 text-[14px] text-muted">
          <Icon name="camera" size={28} />
          Select a photo in the queue, or drop new ones here.
        </div>
      {/if}
    </section>

    <!-- Slot for J6.7: the AI draft form. -->
    <aside aria-label="Draft" class="hidden w-[340px] flex-none border-l border-divider bg-surface lg:block">
      <DraftFormSlot itemId={group?.id ?? null} uploaded={group?.state === 'uploaded'} />
    </aside>
  </div>
</FlowFrame>

<Picker bind:this={picker} onfiles={(f, tag) => (tag === 'add' ? addToSelected(f) : receive(f))} />
<PlaceDialog bind:open={placeOpen} title="Where do new items go?" value={session.place} allowNone onpick={(p) => session.setPlace(p)} />

<Dialog bind:open={askOpen} title="{asking?.length ?? 0} photos">
  <p class="m-0 text-[14px]">Are these photos of one item, or one item per photo?</p>
  {#snippet actions()}
    <Button onclick={() => asking && add(asking, true)}>One per photo</Button>
    <Button variant="primary" onclick={() => asking && add(asking, false)}>One item</Button>
  {/snippet}
</Dialog>

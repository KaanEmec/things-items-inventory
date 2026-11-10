<script lang="ts">
  import { untrack } from 'svelte'
  import { ApiError } from '../api'
  import { can } from '../auth.svelte'
  import { explain } from '../errors'
  import { readReceipt, warrantySourceText } from '../drafts/drafts'
  import { ago } from '../format'
  import Icon from '../Icon.svelte'
  import { navigate, router } from '../router.svelte'
  import { stream } from '../stream.svelte'
  import FlowFrame from '../shell/FlowFrame.svelte'
  import Avatar from '../ui/Avatar.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import Popover from '../ui/Popover.svelte'
  import StatusPill from '../ui/StatusPill.svelte'
  import { showToast } from '../ui/toasts.svelte'
  import { categoryLabel, humanize, valueLabel } from './categories'
  import { attrValue, blank, fromMinor, toMinor } from './form'
  import History from './History.svelte'
  import InlineField from './InlineField.svelte'
  import ItemForm from './ItemForm.svelte'
  import { confirmItem, deleteItem, getItem, markSeen, moveItem, patchItem, restoreItem } from './items'
  import type { ItemList } from './list.svelte'
  import PhotoStrip from './PhotoStrip.svelte'
  import { photoUrl, moveLink, relink, removeLink, toJpeg, uploadItemPhoto } from './photos'
  import PlaceDialog from './PlaceDialog.svelte'
  import type { Catalog, Item, ItemPhoto, PlaceShort } from './types'
  import { warrantyLine } from './warranty'

  interface Props {
    id: string
    list: ItemList
    catalog: Catalog
    /** Full-screen flow with a bottom action bar (Move, Edit, More) instead of the docked panel. */
    phone?: boolean
    /** Called after the item was deleted instead of going back to the items list. */
    onremoved?: () => void
  }
  let { id, list, catalog, phone = false, onremoved }: Props = $props()

  let item = $state<Item | null>(null)
  let loadError = $state('')
  let photoBusy = $state(false)
  let moveOpen = $state(false)
  let editOpen = $state(false)
  let statusOpen = $state(false)
  let moreOpen = $state(false)
  const canEdit = $derived(can('member'))

  $effect(() => {
    const cur = id
    item = untrack(() => list.find(cur)) ?? null
    loadError = ''
    getItem(cur).then(
      (it) => {
        if (cur !== id) return
        setItem(it)
      },
      (e) => {
        if (cur === id) loadError = explain(e).message
      },
    )
  })

  // Another member or a background job changed this item: show it, unless our copy is newer.
  // Fields being edited keep their own draft (InlineField), so typing is never overwritten.
  $effect(() => {
    const cur = id
    return stream.onBurst(
      ['item', 'job', 'resync'],
      () => {
        getItem(cur).then(
          (it) => {
            if (cur !== id || (item && it.updated_at < item.updated_at)) return
            setItem(it)
          },
          (e) => {
            if (cur === id && e instanceof ApiError && e.status === 404) loadError = explain(e).message
          },
        )
      },
      (m) => m.type === 'resync' || (m.type === 'item' ? m.id === cur : m.item_id === cur),
    )
  })

  function setItem(it: Item) {
    item = it
    list.replace(it)
  }

  /** PATCH with an undo toast. Throws so InlineField can show field errors. */
  async function change(body: Record<string, unknown>, undo: Record<string, unknown>, label: string) {
    const next = await patchItem(item!.id, body)
    setItem(next)
    showToast(`${label} saved`, 'success', 6000, {
      label: 'Undo',
      run: async () => {
        try {
          setItem(await patchItem(next.id, undo))
        } catch (e) {
          showToast(explain(e).message, 'error')
        }
      },
    })
  }
  async function guarded(fn: () => Promise<void>) {
    try {
      await fn()
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }

  const core = (key: 'brand' | 'model' | 'serial' | 'color' | 'notes' | 'condition' | 'name', label: string) => (text: string) =>
    change({ [key]: blank(text) }, { [key]: item![key] }, label)
  const ai = (path: string) => !!item?.ai_fields.includes(path)
  const attrText = (v: unknown) => (Array.isArray(v) ? v.map((x) => valueLabel(String(x))).join(', ') : v == null ? '' : String(v))

  const category = $derived(catalog.categories.find((c) => c.key === item?.category))
  const status = (s: string) => guarded(() => change({ status: s }, { status: item!.status }, 'Status'))
  const warranty = $derived(warrantyLine(item?.warranty.end))
  const typeAttr = $derived(typeof item?.attributes.type === 'string' ? valueLabel(item.attributes.type) : '')
  const subline = $derived(
    item ? [categoryLabel(item.category) + (typeAttr ? ` › ${typeAttr}` : ''), [item.brand, item.model].filter(Boolean).join(' ')].filter(Boolean).join(' · ') : '',
  )

  function moveTo(p: PlaceShort | null) {
    const prev = item!.place?.id ?? null
    guarded(async () => {
      const next = await moveItem(item!.id, p?.id ?? null)
      setItem(next)
      showToast(p ? `Moved to ${p.name}` : 'Marked as not placed', 'success', 6000, {
        label: 'Undo',
        run: () => guarded(async () => setItem(await moveItem(next.id, prev))),
      })
    })
  }

  async function remove() {
    const it = item!
    moreOpen = false
    await guarded(async () => {
      await deleteItem(it.id)
      const at = list.remove(it.id)
      if (onremoved) onremoved()
      else navigate(`/items${router.search}`, true)
      showToast(`Deleted ${it.name}`, 'info', 7000, {
        label: 'Undo',
        run: () =>
          guarded(async () => {
            const back = await restoreItem(it.id)
            list.insert(back, at)
          }),
      })
    })
  }

  const confirm = () => guarded(async () => setItem(await confirmItem(item!.id)))
  const seen = () => {
    moreOpen = false
    return guarded(async () => {
      setItem(await markSeen(item!.id))
      showToast('Marked as seen here', 'success')
    })
  }

  // Receipt: kept under Purchase; the AI reads it when it is set up, otherwise the fields are typed.
  let receiptInput = $state<HTMLInputElement>()
  async function addReceipt() {
    const f = receiptInput?.files?.[0]
    if (receiptInput) receiptInput.value = ''
    if (!f) return
    await guarded(async () => {
      await uploadItemPhoto(item!.id, await toJpeg(f), 'receipt')
      setItem(await getItem(item!.id))
      try {
        await readReceipt(item!.id)
        showToast('Receipt added. The AI is reading it.')
      } catch {
        showToast('Receipt added. Type the purchase details by hand.')
      }
    })
  }
  async function copySerial() {
    try {
      await navigator.clipboard.writeText(item!.serial ?? '')
      showToast('Serial copied', 'success')
    } catch {
      showToast('Copy failed. Select the serial and copy it by hand.', 'error')
    }
  }

  // Photos
  async function addPhotos(files: File[]) {
    photoBusy = true
    let failed = ''
    for (const f of files) {
      try {
        await uploadItemPhoto(item!.id, await toJpeg(f))
      } catch (e) {
        failed = e instanceof Error ? e.message : explain(e).message
      }
    }
    if (failed) showToast(failed, 'error')
    await guarded(async () => setItem(await getItem(item!.id)))
    photoBusy = false
  }
  const gallery = () => item!.photos.filter((p) => p.role === 'item').sort((a, b) => a.sort - b.sort)
  async function reorder(p: ItemPhoto, to: number) {
    photoBusy = true
    await guarded(async () => {
      await moveLink(p.link_id, to)
      setItem(await getItem(item!.id))
    })
    photoBusy = false
  }
  const movePhoto = (p: ItemPhoto, step: -1 | 1) => reorder(p, gallery().findIndex((x) => x.link_id === p.link_id) + step)
  async function deletePhoto(p: ItemPhoto) {
    photoBusy = true
    await guarded(async () => {
      await removeLink(p.link_id)
      setItem(await getItem(item!.id))
      showToast('Photo removed', 'info', 7000, {
        label: 'Undo',
        run: () =>
          guarded(async () => {
            await relink(p.photo_id, item!.id, p.role)
            setItem(await getItem(item!.id))
          }),
      })
    })
    photoBusy = false
  }

  async function saveForm(body: Record<string, unknown>) {
    if (Object.keys(body).length) {
      const next = await patchItem(item!.id, body)
      setItem(next)
      showToast('Item saved', 'success')
    }
    editOpen = false
  }

  const menu = 'flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-left text-[14px] hover:bg-chrome-hover md:h-9'
  const statuses = $derived(catalog.statuses)
</script>

{#snippet statusMenu()}
  {#each statuses as s (s)}
    <button type="button" class={menu} onclick={() => ((statusOpen = moreOpen = false), status(s))}>
      <StatusPill status={s} />{#if item?.status === s}<Icon name="check" class="ml-auto text-accent" />{/if}
    </button>
  {/each}
{/snippet}

{#snippet body()}
  {#if !item}
    <p class="m-0 p-6 text-[14px] {loadError ? 'text-danger' : 'text-muted'}" role={loadError ? 'alert' : undefined}>{loadError || 'Loading'}</p>
  {:else}
    {@const it = item}
    <PhotoStrip photos={it.photos.filter((p) => p.role !== 'receipt')} category={it.category} canEdit={canEdit} busy={photoBusy} onadd={addPhotos} onmove={movePhoto} onmain={(p) => reorder(p, 0)} ondelete={deletePhoto} class="bg-surface" />

    {#if canEdit && (it.draft || it.ai_fields.length)}
      <div class="flex flex-wrap items-center gap-3 border-b border-divider bg-ai-field-bg px-4 py-3 md:px-6">
        <Icon name="sparkle" class="text-ai-fg" />
        <p class="m-0 min-w-0 flex-1 text-[13px] text-ai-fg">
          {it.ai_fields.length ? `AI filled ${it.ai_fields.length} ${it.ai_fields.length === 1 ? 'field' : 'fields'}. Change a field or confirm them all.` : 'This item is a draft. Check it, then confirm.'}
        </p>
        <Button onclick={confirm} icon="check">Confirm</Button>
      </div>
    {/if}

    <div class="flex flex-col gap-2.5 border-b border-divider bg-surface px-4 py-4 md:px-6">
      <div class="flex flex-wrap gap-1.5">
        <StatusPill status={it.status} />
        {#if it.draft}<StatusPill status="ai-draft" />{/if}
        {#if warranty}
          <span class="inline-flex h-6 items-center gap-[5px] rounded-full pr-[9px] pl-[7px] text-[12px] font-semibold {warranty.active ? 'bg-use-bg text-use-fg' : 'bg-stored-bg text-stored-fg'}">
            <Icon name="shield" size={13} />{warranty.text}
          </span>
        {/if}
      </div>
      <div>
        <h2 class="m-0 text-[26px] font-extrabold tracking-[-0.025em] md:text-[22px] md:font-bold md:tracking-[-0.015em]">{it.name}{#if it.quantity > 1}<span class="font-mono text-[15px] font-medium text-muted"> ×{it.quantity}</span>{/if}</h2>
        <div class="mt-0.5 font-mono text-[12px] text-muted">{subline}</div>
      </div>
      {#if canEdit && !phone}
        <div class="flex gap-1.5">
          <Button variant="primary" icon="move" onclick={() => (moveOpen = true)}>Move</Button>
          <Popover bind:open={statusOpen}>
            {#snippet trigger({ toggle, open })}
              <Button icon="tag" onclick={toggle} aria-expanded={open}>Status</Button>
            {/snippet}
            {@render statusMenu()}
          </Popover>
          <Button icon="edit" onclick={() => (editOpen = true)}>Edit</Button>
          <Popover bind:open={moreOpen} align="right">
            {#snippet trigger({ toggle, open })}
              <Button icon="more" onclick={toggle} aria-expanded={open} aria-label="More" class="w-9 px-0" />
            {/snippet}
            <button type="button" class={menu} onclick={seen}><Icon name="eye" class="text-muted" />I saw it here</button>
            <button type="button" class="{menu} text-danger" onclick={remove}><Icon name="trash" />Delete item</button>
          </Popover>
        </div>
      {/if}
    </div>

    <h3 class="m-0 flex h-11 items-center gap-2 border-b border-divider bg-surface-subtle px-4 text-[13px] font-semibold md:px-6"><Icon name="pin" class="text-label" />Where it is</h3>
    <button type="button" disabled={!canEdit} onclick={() => (moveOpen = true)} class="flex w-full cursor-pointer items-center gap-3 border-b border-divider bg-surface px-4 py-3 text-left disabled:cursor-default md:px-6">
      <span class="flex size-12 flex-none items-center justify-center rounded-[12px] bg-stored-bg text-stored-fg"><Icon name="pin" size={20} /></span>
      <span class="min-w-0 flex-1">
        <b class="block truncate text-[16px] font-semibold md:text-[15px]">{it.place?.name ?? 'Not placed'}</b>
        <span class="block truncate text-[13px] text-muted">{it.place ? it.place.path.split(' · ').slice(1).reverse().join(' › ') || 'Top level' : 'Move it to a place to find it again'}</span>
      </span>
      <span class="flex flex-none items-center gap-1.5 text-[12px] text-muted">
        {#if it.updated_by}<Avatar name={it.updated_by.name} size={22} />{/if}{ago(it.last_seen_at)}
      </span>
    </button>

    <h3 class="m-0 flex h-11 items-center gap-2 border-b border-divider bg-surface-subtle px-4 text-[13px] font-semibold md:px-6"><Icon name="tag" class="text-label" />Details</h3>
    <div class="flex flex-col border-b border-divider bg-surface py-1">
      <InlineField label="Name" value={it.name} ai={ai('name')} locked={!canEdit} errorKeys={['name']} save={core('name', 'Name')} />
      <InlineField
        label="Category"
        kind="select"
        value={it.category}
        display={categoryLabel(it.category)}
        options={catalog.categories.map((c) => ({ value: c.key, label: categoryLabel(c.key) }))}
        ai={ai('category')}
        locked={!canEdit}
        errorKeys={['category']}
        save={(t) => change({ category: t }, { category: it.category }, 'Category')}
      />
      <InlineField label="Brand" value={it.brand ?? ''} ai={ai('brand')} locked={!canEdit} errorKeys={['brand']} save={core('brand', 'Brand')} />
      <InlineField label="Model" mono value={it.model ?? ''} ai={ai('model')} locked={!canEdit} errorKeys={['model']} save={core('model', 'Model')} />
      <InlineField label="Serial" mono value={it.serial ?? ''} ai={ai('serial')} locked={!canEdit} errorKeys={['serial']} save={core('serial', 'Serial')} />
      {#if it.serial}
        <div class="px-4 pb-1 md:px-6"><Button class="h-8" icon="copy" onclick={copySerial}>Copy serial</Button></div>
      {/if}
      <InlineField label="Color" value={it.color ?? ''} ai={ai('color')} locked={!canEdit} errorKeys={['color']} save={core('color', 'Color')} />
      <InlineField label="Quantity" kind="int" value={String(it.quantity)} ai={ai('quantity')} locked={!canEdit} errorKeys={['quantity']} save={(t) => change({ quantity: t === '' ? null : Number.isInteger(Number(t)) ? Number(t) : t }, { quantity: it.quantity }, 'Quantity')} />
      <InlineField label="Condition" kind="select" value={it.condition ?? ''} display={it.condition ? valueLabel(it.condition) : ''} options={[{ value: '', label: 'Not set' }, ...catalog.conditions.map((c) => ({ value: c, label: valueLabel(c) }))]} ai={ai('condition')} locked={!canEdit} errorKeys={['condition']} save={core('condition', 'Condition')} />
      <InlineField label="Notes" kind="textarea" value={it.notes ?? ''} ai={ai('notes')} locked={!canEdit} errorKeys={['notes']} save={core('notes', 'Notes')} />
    </div>

    {#if category?.attributes.length}
      <h3 class="m-0 flex h-11 items-center gap-2 border-b border-divider bg-surface-subtle px-4 text-[13px] font-semibold md:px-6"><Icon name="chip" class="text-label" />{categoryLabel(category.key)} details</h3>
      <div class="flex flex-col border-b border-divider bg-surface py-1">
        {#each category.attributes as a (a.key)}
          {@const raw = it.attributes[a.key]}
          <InlineField
            label={humanize(a.key)}
            kind={a.type === 'enum' ? 'select' : a.type === 'int' ? 'int' : a.type === 'number' ? 'number' : 'text'}
            options={a.type === 'enum' ? [{ value: '', label: 'Not set' }, ...(a.values ?? []).map((x) => ({ value: x, label: valueLabel(x) }))] : []}
            value={Array.isArray(raw) ? attrText(raw) : raw == null ? '' : String(raw)}
            display={a.type === 'enum' || a.type === 'multi' || a.type === 'bool' ? attrText(raw === true ? 'Yes' : raw) : undefined}
            locked={!canEdit || a.type === 'multi' || a.type === 'bool'}
            ai={ai('attributes.' + a.key)}
            errorKeys={['attributes.' + a.key, a.key]}
            save={(t) => change({ attributes: { [a.key]: attrValue(a, t) } }, { attributes: { [a.key]: raw ?? null } }, humanize(a.key))}
          />
        {/each}
      </div>
    {/if}

    <h3 class="m-0 flex h-11 items-center gap-2 border-b border-divider bg-surface-subtle px-4 text-[13px] font-semibold md:px-6"><Icon name="receipt" class="text-label" />Purchase and warranty
      {#if canEdit}
        <Button class="ml-auto h-8" icon="camera" onclick={() => receiptInput?.click()}>Add receipt</Button>
        <input bind:this={receiptInput} type="file" accept="image/*" class="hidden" onchange={addReceipt} tabindex="-1" aria-hidden="true" />
      {/if}
    </h3>
    <div class="flex flex-col bg-surface py-1">
      <InlineField label="Bought on" kind="date" value={it.purchase.date?.slice(0, 10) ?? ''} ai={ai('purchase_date')} locked={!canEdit} errorKeys={['purchase_date', 'purchase.date']} save={(t) => change({ purchase: { date: blank(t) } }, { purchase: { date: it.purchase.date } }, 'Purchase date')} />
      <InlineField
        label="Price"
        kind="number"
        value={fromMinor(it.purchase.price_minor)}
        display={it.purchase.price_minor == null ? '' : `${fromMinor(it.purchase.price_minor)} ${it.purchase.currency ?? ''}`.trim()}
        ai={ai('price_minor')}
        locked={!canEdit}
        errorKeys={['price_minor', 'purchase.price_minor']}
        save={(t) => change({ purchase: { price_minor: toMinor(t) } }, { purchase: { price_minor: it.purchase.price_minor } }, 'Price')}
      />
      <InlineField label="Seller" value={it.purchase.seller ?? ''} ai={ai('seller')} locked={!canEdit} errorKeys={['seller', 'purchase.seller']} save={(t) => change({ purchase: { seller: blank(t) } }, { purchase: { seller: it.purchase.seller } }, 'Seller')} />
      <InlineField label="Warranty until" kind="date" value={it.warranty.end?.slice(0, 10) ?? ''} ai={ai('warranty_end')} locked={!canEdit} errorKeys={['warranty_end', 'warranty.end']} save={(t) => change({ warranty: { end: blank(t) } }, { warranty: { end: it.warranty.end } }, 'Warranty')} />
      {#if warrantySourceText(it.warranty.source) && it.warranty.end}
        <p class="m-0 px-4 pb-2 text-[12px] text-muted md:px-6" data-testid="warranty-source">{warrantySourceText(it.warranty.source)}</p>
      {/if}
      {#each it.photos.filter((p) => p.role === 'receipt') as r (r.link_id)}
        <a href={photoUrl(r.sha256, 'screen')} target="_blank" rel="noreferrer" class="mx-4 mb-2 block w-fit md:mx-6"><img src={photoUrl(r.sha256)} alt="Receipt" class="h-20 rounded-control border border-border" /></a>
      {/each}
    </div>

    <History itemId={it.id} />
  {/if}
{/snippet}

{#if phone}
  <FlowFrame icon="tag" title={item?.name ?? 'Item'} breadcrumb={['Items']}>
    {@render body()}
    {#snippet bar()}
      {#if item && canEdit}
        <div class="grid flex-1 grid-cols-[2fr_1fr_1fr] gap-2">
          <Button variant="primary" icon="move" class="h-14 rounded-[14px] text-[15px]" onclick={() => (moveOpen = true)}>Move</Button>
          <Button icon="edit" class="h-14 rounded-[14px]" onclick={() => (editOpen = true)}>Edit</Button>
          <Popover bind:open={moreOpen} align="right" up>
            {#snippet trigger({ toggle, open })}
              <Button icon="more" class="h-14 w-full rounded-[14px]" onclick={toggle} aria-expanded={open}>More</Button>
            {/snippet}
            <div class="px-2.5 pt-1 pb-1 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">Status</div>
            {@render statusMenu()}
            <button type="button" class={menu} onclick={seen}><Icon name="eye" class="text-muted" />I saw it here</button>
            <button type="button" class="{menu} text-danger" onclick={remove}><Icon name="trash" />Delete item</button>
          </Popover>
        </div>
      {/if}
    {/snippet}
  </FlowFrame>
{:else}
  <div class="flex min-h-0 flex-1 flex-col overflow-y-auto">{@render body()}</div>
{/if}

{#if item}
  <PlaceDialog bind:open={moveOpen} title="Move {item.name}" value={item.place} allowNone onpick={moveTo} />
  <Dialog bind:open={editOpen} title="Edit item" size="lg">
    {#if editOpen}<ItemForm id="item-edit-form" {catalog} {item} save={saveForm} />{/if}
    {#snippet actions()}
      <Button onclick={() => (editOpen = false)}>Cancel</Button>
      <Button variant="primary" type="submit" form="item-edit-form">Save</Button>
    {/snippet}
  </Dialog>
{/if}

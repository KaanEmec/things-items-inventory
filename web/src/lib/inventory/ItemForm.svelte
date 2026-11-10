<script lang="ts">
  import { ApiError } from '../api'
  import Icon from '../Icon.svelte'
  import Field from '../ui/Field.svelte'
  import FormError from '../ui/FormError.svelte'
  import Select from '../ui/Select.svelte'
  import { carryAttributes, categoryLabel, humanize, valueLabel } from './categories'
  import { attrsToForm, attrValue, blank, coreFields, toMinor, type CoreFields } from './form'
  import PlaceDialog from './PlaceDialog.svelte'
  import type { Catalog, Item, PlaceShort } from './types'

  interface Props {
    catalog: Catalog
    /** Editing this item; leave out to create a new one. */
    item?: Item
    /** The id of the <form>, so a dialog's action bar can submit it (`<Button type="submit" form={id}>`). */
    id: string
    /** Receives the JSON body (only changed fields when editing). Throw an ApiError to show it on the fields. */
    save: (body: Record<string, unknown>) => Promise<void>
    /** Preselected place for a new item. */
    place?: PlaceShort | null
  }
  let { catalog, item, id, save, place: startPlace = null }: Props = $props()

  // The form is seeded once from the item it was opened for.
  /* svelte-ignore state_referenced_locally */
  const start: CoreFields = item
    ? coreFields(item)
    : { name: '', category: 'gadget', status: 'stored', brand: '', model: '', serial: '', color: '', quantity: '1', condition: '', notes: '', purchase_date: '', price: '', seller: '', warranty_end: '' }
  let v = $state<CoreFields>({ ...start })
  /* svelte-ignore state_referenced_locally */
  let attrs = $state<Record<string, unknown>>(attrsToForm(item?.attributes ?? {}))
  /* svelte-ignore state_referenced_locally */
  let place = $state<PlaceShort | null>(item?.place ?? startPlace)
  let pickPlace = $state(false)
  let fieldErrors = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)

  const category = $derived(catalog.categories.find((c) => c.key === v.category))
  const err = (...keys: string[]) => keys.map((k) => fieldErrors[k]).find(Boolean)

  // Changing the category keeps the attributes the new one also has.
  function setCategory(key: string) {
    attrs = carryAttributes(attrs, catalog.categories.find((c) => c.key === key))
    v.category = key
  }
  const str = (k: string) => (typeof attrs[k] === 'string' ? (attrs[k] as string) : '')
  const list = (k: string) => (Array.isArray(attrs[k]) ? (attrs[k] as string[]) : [])
  function toggle(k: string, val: string, on: boolean) {
    const cur = list(k).filter((x) => x !== val)
    attrs[k] = on ? [...cur, val] : cur
  }

  function body(): Record<string, unknown> {
    const out: Record<string, unknown> = {}
    const had = item ? coreFields(item) : start
    const changed = (k: keyof CoreFields) => !item || v[k] !== had[k]
    if (changed('name')) out.name = v.name.trim()
    if (changed('category')) out.category = v.category
    if (changed('status')) out.status = v.status
    for (const k of ['brand', 'model', 'serial', 'color', 'condition', 'notes'] as const) {
      if (changed(k) && (item || blank(v[k]) !== null)) out[k] = blank(v[k])
    }
    if (changed('quantity')) out.quantity = Number.isInteger(Number(v.quantity)) && v.quantity.trim() !== '' ? Number(v.quantity) : v.quantity
    const purchase: Record<string, unknown> = {}
    if (changed('purchase_date') && (item || v.purchase_date)) purchase.date = blank(v.purchase_date)
    if (changed('price') && (item || v.price.trim())) purchase.price_minor = toMinor(v.price)
    if (changed('seller') && (item || v.seller.trim())) purchase.seller = blank(v.seller)
    if (Object.keys(purchase).length) out.purchase = purchase
    if (changed('warranty_end') && (item || v.warranty_end)) out.warranty = { end: blank(v.warranty_end) }
    if (!item && place) out.place_id = place.id

    const a: Record<string, unknown> = {}
    const before = attrsToForm(item?.attributes ?? {})
    for (const def of category?.attributes ?? []) {
      const now = attrValue(def, attrs[def.key])
      const was = attrValue(def, before[def.key])
      if (JSON.stringify(now) !== JSON.stringify(was)) a[def.key] = now
    }
    if (Object.keys(a).length) out.attributes = a
    return out
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (busy) return
    busy = true
    fieldErrors = {}
    message = ''
    try {
      await save(body())
    } catch (x) {
      if (x instanceof ApiError) {
        fieldErrors = x.fields
        message = Object.keys(x.fields).length ? '' : x.message
      } else message = 'Something went wrong. Try again.'
    } finally {
      busy = false
    }
  }
  const textarea = 'min-h-20 w-full rounded-control border border-border-input bg-surface px-3 py-2 text-[16px] md:text-[14px]'
</script>

<form {id} onsubmit={submit} class="flex flex-col gap-4">
  <FormError {message} />
  <Field label="Name" bind:value={v.name} error={err('name')} required autocomplete="off" />
  <div class="grid gap-4 sm:grid-cols-2">
    <Select label="Category" bind:value={() => v.category, setCategory} options={catalog.categories.map((c) => ({ value: c.key, label: categoryLabel(c.key) }))} />
    <Select label="Status" bind:value={v.status} options={catalog.statuses.map((s) => ({ value: s, label: valueLabel(s) }))} />
  </div>

  {#if !item}
    <div class="flex flex-col gap-1.5">
      <span class="text-[13px] font-medium text-body">Place</span>
      <button type="button" onclick={() => (pickPlace = true)} class="flex h-11 cursor-pointer items-center gap-2 rounded-control border border-border-input bg-surface px-3 text-left text-[14px] md:h-9 {err('place_id') ? 'border-danger' : ''}">
        <Icon name="pin" class="text-muted" /><span class="min-w-0 flex-1 truncate {place ? '' : 'text-faint'}">{place?.path ?? 'Not placed'}</span>
        <Icon name="chevron-right" class="text-muted" />
      </button>
      {#if err('place_id')}<p class="m-0 text-[12px] text-danger">{err('place_id')}</p>{/if}
    </div>
    <PlaceDialog bind:open={pickPlace} value={place} allowNone onpick={(p) => (place = p)} />
  {/if}

  <div class="grid gap-4 sm:grid-cols-2">
    <Field label="Brand" bind:value={v.brand} error={err('brand')} />
    <Field label="Model" bind:value={v.model} error={err('model')} />
    <Field label="Serial" bind:value={v.serial} error={err('serial')} />
    <Field label="Color" bind:value={v.color} error={err('color')} />
    <Field label="Quantity" bind:value={v.quantity} error={err('quantity')} inputmode="numeric" />
    <Select label="Condition" bind:value={v.condition} options={[{ value: '', label: 'Not set' }, ...catalog.conditions.map((c) => ({ value: c, label: valueLabel(c) }))]} />
  </div>

  {#if category?.attributes.length}
    <fieldset class="m-0 flex flex-col gap-4 border-0 p-0">
      <legend class="mb-3 p-0 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">{categoryLabel(category.key)} details</legend>
      <div class="grid gap-4 sm:grid-cols-2">
        {#each category.attributes as a (a.key)}
          {@const e = err('attributes.' + a.key, a.key)}
          {#if a.type === 'enum'}
            <Select label={humanize(a.key)} bind:value={() => str(a.key), (x) => (attrs[a.key] = x)} options={[{ value: '', label: 'Not set' }, ...(a.values ?? []).map((x) => ({ value: x, label: valueLabel(x) }))]} />
          {:else if a.type === 'multi'}
            <fieldset class="m-0 border-0 p-0 sm:col-span-2">
              <legend class="mb-1.5 p-0 text-[13px] font-medium text-body">{humanize(a.key)}</legend>
              <div class="flex flex-wrap gap-2">
                {#each a.values ?? [] as x (x)}
                  <label class="inline-flex h-11 cursor-pointer items-center gap-2 rounded-control border border-border-input bg-surface px-3 text-[14px] md:h-9">
                    <input type="checkbox" checked={list(a.key).includes(x)} onchange={(ev) => toggle(a.key, x, ev.currentTarget.checked)} class="accent-accent" />{valueLabel(x)}
                  </label>
                {/each}
              </div>
              {#if e}<p class="m-0 mt-1 text-[12px] text-danger">{e}</p>{/if}
            </fieldset>
          {:else if a.type === 'bool'}
            <label class="flex h-11 items-center gap-2 text-[14px]"><input type="checkbox" checked={attrs[a.key] === true} onchange={(ev) => (attrs[a.key] = ev.currentTarget.checked)} class="accent-accent" />{humanize(a.key)}</label>
          {:else}
            <Field label={humanize(a.key)} hint={a.hint} error={e} bind:value={() => str(a.key), (x) => (attrs[a.key] = x)} inputmode={a.type === 'int' ? 'numeric' : a.type === 'number' ? 'decimal' : undefined} />
          {/if}
        {/each}
      </div>
    </fieldset>
  {/if}

  <fieldset class="m-0 flex flex-col gap-4 border-0 p-0">
    <legend class="mb-3 p-0 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">Purchase and warranty</legend>
    <div class="grid gap-4 sm:grid-cols-2">
      <Field label="Bought on" type="date" bind:value={v.purchase_date} error={err('purchase_date', 'purchase.date')} />
      <Field label="Price" bind:value={v.price} error={err('price_minor', 'purchase.price_minor')} inputmode="decimal" />
      <Field label="Seller" bind:value={v.seller} error={err('seller', 'purchase.seller')} />
      <Field label="Warranty until" type="date" bind:value={v.warranty_end} error={err('warranty_end', 'warranty.end')} />
    </div>
  </fieldset>

  <div class="flex flex-col gap-1.5">
    <label for="{id}-notes" class="text-[13px] font-medium text-body">Notes</label>
    <textarea id="{id}-notes" bind:value={v.notes} class="{textarea} {err('notes') ? 'border-danger' : ''}"></textarea>
    {#if err('notes')}<p class="m-0 text-[12px] text-danger">{err('notes')}</p>{/if}
  </div>
  <button type="submit" hidden disabled={busy}>Save</button>
</form>

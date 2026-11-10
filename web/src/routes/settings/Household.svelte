<script lang="ts">
  import { api } from '../../lib/api'
  import { auth, can } from '../../lib/auth.svelte'
  import { explain } from '../../lib/errors'
  import { currencies, languages } from '../../lib/format'
  import Button from '../../lib/ui/Button.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import Select from '../../lib/ui/Select.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'
  import Block from './Block.svelte'

  // GET /household: { name, language, currency, default_warranty_months, ai_paused }
  let name = $state('')
  let language = $state('en')
  let currency = $state('EUR')
  let warranty = $state('24')
  let loaded = $state(false)
  let fields = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)
  const editable = $derived(can('admin'))
  const currencyOptions = $derived(currencies.some((c) => c.value === currency) ? currencies : [...currencies, { value: currency, label: currency }])
  const languageOptions = $derived(languages.some((l) => l.value === language) ? languages : [...languages, { value: language, label: language }])

  $effect(() => {
    auth.householdId
    loaded = false
    api
      .get<any>('/api/v1/household')
      .then((h) => {
        name = h.name ?? ''
        language = h.language ?? 'en'
        currency = h.currency ?? 'EUR'
        warranty = String(h.default_warranty_months ?? 24)
        loaded = true
      })
      .catch((e) => (message = explain(e).message))
  })

  async function save(e: SubmitEvent) {
    e.preventDefault()
    message = ''
    fields = {}
    busy = true
    try {
      await api.patch('/api/v1/household', { name, language, currency, default_warranty_months: Number(warranty) })
      const h = auth.households.find((x) => x.id === auth.householdId)
      if (h) h.name = name
      showToast('Household saved', 'success')
    } catch (err) {
      ;({ fields, message } = explain(err))
    } finally {
      busy = false
    }
  }
</script>

<Block title="Household" note={editable ? undefined : 'Only owners and admins can change these.'}>
  <FormError {message} />
  {#if loaded}
    {#if editable}
      <form onsubmit={save} class="flex flex-col gap-4" novalidate>
        <Field label="Name" bind:value={name} error={fields.name} required />
        <div class="grid gap-4 sm:grid-cols-2">
          <Select label="Language" bind:value={language} options={languageOptions} />
          <Select label="Currency" bind:value={currency} options={currencyOptions} />
        </div>
        <Field label="Default warranty (months)" type="number" min="0" inputmode="numeric" bind:value={warranty} error={fields.default_warranty_months} hint="Used for new items without a warranty of their own." />
        <div><Button variant="primary" type="submit" disabled={busy}>{busy ? 'Saving' : 'Save'}</Button></div>
      </form>
    {:else}
      <dl class="m-0 grid grid-cols-[auto_1fr] gap-x-6 gap-y-2.5 text-[14px]">
        <dt class="text-muted">Name</dt><dd class="m-0">{name}</dd>
        <dt class="text-muted">Language</dt><dd class="m-0">{languageOptions.find((l) => l.value === language)?.label}</dd>
        <dt class="text-muted">Currency</dt><dd class="m-0">{currency}</dd>
        <dt class="text-muted">Default warranty</dt><dd class="m-0 font-mono">{warranty} months</dd>
      </dl>
    {/if}
  {:else if !message}
    <p class="m-0 text-[14px] text-muted">Loading</p>
  {/if}
</Block>

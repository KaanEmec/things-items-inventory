<script lang="ts">
  import { ai, type AIProfile, type AIRole } from '../../lib/ai.svelte'
  import { api } from '../../lib/api'
  import { explain } from '../../lib/errors'
  import Icon from '../../lib/Icon.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Dialog from '../../lib/ui/Dialog.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'
  import AiChip from './AiChip.svelte'

  interface Props {
    role: AIRole
    profile: AIProfile
    editable: boolean
  }
  let { role, profile, editable }: Props = $props()

  const OPENROUTER = 'https://openrouter.ai/api/v1'
  const copy = {
    vision: { title: 'Photo understanding', icon: 'camera', purpose: 'Reads item, label, receipt and contents photos and fills in the details.' },
    text: { title: 'Everything else', icon: 'sparkle', purpose: 'Completes quick-add rows, turns pasted text into items and maps CSV imports.' },
  } as const
  const stepLabels: Record<string, string> = {
    reach: 'Reach the endpoint',
    token: 'Check the token',
    model: 'Find the model',
    images: 'Send a test photo',
    json: 'Get a structured answer',
  }

  interface Step { id: string; ok: boolean | null; message: string; ms?: number }
  interface Model { id: string; name: string; vision: boolean; vision_known: boolean }

  let baseUrl = $state(OPENROUTER)
  let model = $state('')
  let token = $state('')
  let same = $state(false)
  let replacing = $state(false)
  let steps = $state<Step[]>([])
  let testing = $state(false)
  let testedKey = $state('')
  let models = $state<Model[]>([])
  let message = $state('')
  let fields = $state<Record<string, string>>({})
  let busy = $state(false)
  let confirmOpen = $state(false)
  let removeOpen = $state(false)
  const listId = $props.id()

  const server = $derived(profile.source === 'server')
  const locked = $derived(!editable || server)
  const showToken = $derived(!profile.token_set || replacing)
  const key = $derived(JSON.stringify([baseUrl, model, token, same]))

  // Start from what is saved whenever the saved values change (not on every reload,
  // so test results stay visible after a test refreshes the chip).
  let savedKey = ''
  $effect(() => {
    const k = JSON.stringify([profile.base_url, profile.model, profile.same_as_vision, profile.token_set, profile.token_hint])
    if (k === savedKey) return
    savedKey = k
    baseUrl = profile.base_url || OPENROUTER
    model = profile.model
    same = role === 'text' && !!profile.same_as_vision
    token = ''
    replacing = false
    fields = {}
  })

  async function loadModels() {
    if (server || !editable || (!token && !profile.token_set) || same) return
    try {
      const r = await api.post<{ models: Model[] }>(`/api/v1/settings/ai/${role}/models`, body(false))
      const list = r.models ?? []
      models = role === 'vision' ? [...list].sort((a, b) => Number(b.vision) - Number(a.vision)) : list
    } catch {
      models = [] // free typing still works
    }
  }
  $effect(() => {
    profile.token_hint
    if (profile.token_set) loadModels()
  })

  function body(withModel = true) {
    return { base_url: baseUrl, ...(withModel ? { model } : {}), ...(token ? { token } : {}) }
  }

  async function test() {
    testing = true
    message = ''
    steps = []
    try {
      const r = await api.post<{ ok: boolean; steps: Step[] }>(`/api/v1/settings/ai/${role}/test`, server ? {} : body())
      steps = r.steps ?? []
      testedKey = r.ok ? key : ''
      if (r.ok) showToast(`${copy[role].title} works`, 'success')
      if (!server) await ai.load()
    } catch (e) {
      message = explain(e).message
    } finally {
      testing = false
    }
  }

  function save() {
    // An untested or failing profile can be saved, but ask once.
    if (!same && testedKey !== key) confirmOpen = true
    else doSave()
  }

  async function doSave() {
    confirmOpen = false
    message = ''
    fields = {}
    busy = true
    try {
      const payload = role === 'text' && same ? { same_as_vision: true } : { ...body(), ...(role === 'text' ? { same_as_vision: false } : {}) }
      await api.put(`/api/v1/settings/ai/${role}`, payload)
      await ai.load()
      showToast(`${copy[role].title} saved`, 'success')
    } catch (e) {
      ;({ fields, message } = explain(e))
    } finally {
      busy = false
    }
  }

  async function remove() {
    removeOpen = false
    try {
      await api.del(`/api/v1/settings/ai/${role}`)
      await ai.load()
      showToast('Saved settings removed', 'success')
    } catch (e) {
      message = explain(e).message
    }
  }
</script>

<section class="border-b border-divider bg-surface max-md:mb-3 max-md:border-t" aria-label={copy[role].title}>
  <div class="flex min-h-12 items-center gap-2 border-b border-divider bg-surface-subtle px-4 md:px-6">
    <Icon name={copy[role].icon} class="text-label" />
    <h3 class="m-0 flex-1 text-[13px] font-semibold">{copy[role].title}</h3>
    <AiChip status={ai.status(role)} since={ai.effective(role)?.failing_since} />
  </div>
  <div class="flex flex-col gap-4 px-4 py-4 md:max-w-2xl md:px-6">
    <p class="m-0 text-[14px] text-muted">{copy[role].purpose}</p>

    {#if !editable}
      <p class="m-0 text-[13px] text-muted">An owner or admin can change this.</p>
    {:else}
      <FormError {message} />
      {#if server}<p class="m-0 text-[13px] font-medium text-muted">Set by the server. Ask whoever runs it to change these.</p>{/if}

      {#if role === 'text'}
        <label class="flex min-h-11 cursor-pointer items-center gap-3 text-[14px] font-medium">
          <input type="checkbox" role="switch" bind:checked={same} disabled={locked} class="size-5 accent-accent" />
          Same as photo understanding
        </label>
      {/if}

      {#if !same}
        <Field label="Base URL" bind:value={baseUrl} error={fields.base_url} disabled={locked} onblur={loadModels} inputmode="url" autocomplete="off" />
        {#if server}
          <Field label="API token" value={profile.token_set ? `saved ····${profile.token_hint}` : 'not set'} disabled />
        {:else if showToken}
          <PasswordField label="API token" bind:value={token} error={fields.token} autocomplete="new-password" hint={replacing ? 'The new token replaces the saved one when you save.' : undefined} />
          {#if replacing}<div><Button variant="ghost" onclick={() => ((replacing = false), (token = ''))}>Keep the saved token</Button></div>{/if}
        {:else}
          <div class="flex flex-col gap-1.5">
            <span class="text-[13px] font-medium text-body">API token</span>
            <div class="flex flex-wrap items-center gap-2">
              <span class="flex h-11 min-w-0 flex-1 items-center rounded-control border border-border-input bg-surface-subtle px-3 font-mono text-[13px] md:h-9">saved ····{profile.token_hint}</span>
              <Button onclick={() => (replacing = true)}>Replace</Button>
              <Button onclick={() => (removeOpen = true)}>Remove</Button>
            </div>
          </div>
        {/if}
        <div>
          <Field label="Model" bind:value={model} error={fields.model} disabled={locked} list={listId} onfocus={loadModels} autocomplete="off" hint={models.length ? undefined : 'Type the model name if the endpoint does not list models.'} />
          <datalist id={listId}>
            {#each models as m (m.id)}<option value={m.id}>{m.name}{role === 'vision' && m.vision ? ' (accepts images)' : ''}</option>{/each}
          </datalist>
        </div>
      {/if}

      <div class="flex flex-wrap items-center gap-2">
        {#if !same}<Button icon="pulse" onclick={test} disabled={testing}>{testing ? 'Testing' : 'Test'}</Button>{/if}
        {#if !server}<Button variant="primary" onclick={save} disabled={busy}>{busy ? 'Saving' : 'Save'}</Button>{/if}
      </div>

      {#if steps.length}
        <ol class="m-0 flex list-none flex-col divide-y divide-divider-row rounded-control border border-border p-0" aria-label="Test steps">
          {#each steps as s (s.id)}
            <li class="flex items-start gap-2.5 px-3 py-2 text-[13px]">
              <Icon
                name={s.ok === true ? 'check' : s.ok === false ? 'x' : 'clock'}
                class="mt-0.5 {s.ok === true ? 'text-use-fg' : s.ok === false ? 'text-repair-fg' : 'text-faint'}"
              />
              <div class="min-w-0 flex-1">
                <div class="font-semibold">{stepLabels[s.id] ?? s.id}{#if s.ok === null}<span class="font-normal text-muted"> (not run)</span>{/if}</div>
                {#if s.message}<div class="text-muted">{s.message}</div>{/if}
              </div>
              {#if s.ms != null && s.ok !== null}<span class="font-mono text-[12px] text-muted">{s.ms} ms</span>{/if}
            </li>
          {/each}
        </ol>
      {/if}
    {/if}
  </div>
</section>

<Dialog bind:open={confirmOpen} title="Save without a passing test?">
  <p class="m-0 text-[14px]">These settings have not passed a test. You can save them anyway and fix them later. AI features stay unavailable until they work.</p>
  {#snippet actions()}
    <Button onclick={() => (confirmOpen = false)}>Back</Button>
    <Button variant="primary" onclick={doSave}>Save anyway</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={removeOpen} title="Remove saved settings">
  <p class="m-0 text-[14px]">Remove the saved URL, token and model for {copy[role].title.toLowerCase()}? The token cannot be recovered.</p>
  {#snippet actions()}
    <Button onclick={() => (removeOpen = false)}>Cancel</Button>
    <Button variant="primary" onclick={remove}>Remove</Button>
  {/snippet}
</Dialog>

<script lang="ts">
  import { api } from '../../lib/api'
  import { auth } from '../../lib/auth.svelte'
  import { explain } from '../../lib/errors'
  import { ago, shortDate } from '../../lib/format'
  import Icon from '../../lib/Icon.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import CopyField from '../../lib/ui/CopyField.svelte'
  import Dialog from '../../lib/ui/Dialog.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import Select from '../../lib/ui/Select.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'

  // GET /tokens: { tokens: [{id, name, scope, hint, last_used_at, expires_at}] }
  // POST /tokens: { token, ...row }
  interface Token {
    id: string
    name: string
    scope: string
    hint: string
    lastUsed: string | null
    expiresAt: string | null
  }
  const scopeLabel = (s: string) => (s === 'read' ? 'Read only' : 'Read and write')
  const toToken = (t: any): Token => ({
    id: String(t.id),
    name: t.name,
    scope: t.scope,
    hint: t.hint ?? t.prefix ?? '',
    lastUsed: t.last_used_at ?? t.last_used ?? null,
    expiresAt: t.expires_at ?? null,
  })

  let tokens = $state<Token[]>([])
  let loaded = $state(false)
  let message = $state('')

  async function load() {
    try {
      const r = await api.get<{ tokens: any[] }>('/api/v1/tokens')
      tokens = (r.tokens ?? []).map(toToken)
      message = ''
    } catch (e) {
      message = explain(e).message
    }
    loaded = true
  }
  $effect(() => {
    auth.householdId
    load()
  })

  // Create
  let createOpen = $state(false)
  let name = $state('')
  let scope = $state('read')
  let days = $state('')
  let fields = $state<Record<string, string>>({})
  let createMessage = $state('')
  let busy = $state(false)
  let shownOpen = $state(false)
  let shownToken = $state('')
  let shownName = $state('')

  function openCreate() {
    name = ''
    scope = 'read'
    days = ''
    fields = {}
    createMessage = ''
    createOpen = true
  }

  async function create() {
    fields = {}
    createMessage = ''
    busy = true
    try {
      const expires_at = days ? new Date(Date.now() + Number(days) * 86400000).toISOString() : undefined
      const r = await api.post<any>('/api/v1/tokens', { name, scope, expires_at })
      createOpen = false
      shownToken = r.token
      shownName = r.name ?? name
      shownOpen = true
      await load()
    } catch (e) {
      ;({ fields, message: createMessage } = explain(e))
    } finally {
      busy = false
    }
  }

  // Revoke
  let revoking = $state<Token | null>(null)
  let revokeOpen = $state(false)
  let revokeError = $state('')
  async function revoke() {
    if (!revoking) return
    revokeError = ''
    try {
      await api.del(`/api/v1/tokens/${revoking.id}`)
      revokeOpen = false
      showToast('Token revoked', 'success')
      await load()
    } catch (e) {
      revokeError = explain(e).message
    }
  }
</script>

<div class="border-b border-divider bg-surface max-md:border-t">
  <div class="flex min-h-12 items-center gap-2 border-b border-divider bg-surface-subtle px-4 md:px-6">
    <h3 class="m-0 flex-1 text-[13px] font-semibold">Access tokens</h3>
    <Button variant="primary" icon="plus" onclick={openCreate}>New token</Button>
  </div>
  <p class="m-0 px-4 pt-3 text-[13px] text-muted md:px-6">An agent or script uses a token instead of your password. It acts with your role in this household, limited by its scope.</p>
  {#if message}<div class="p-4 md:px-6"><FormError {message} /></div>{/if}
  {#if !loaded}
    <p class="m-0 px-4 py-4 text-[14px] text-muted md:px-6">Loading</p>
  {:else if !tokens.length && !message}
    <p class="m-0 px-4 py-4 text-[14px] text-muted md:px-6">No access tokens yet.</p>
  {/if}
  <ul class="m-0 mt-2 list-none divide-y divide-divider-row border-t border-divider-row p-0">
    {#each tokens as t (t.id)}
      <li class="flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-3 md:px-6">
        <Icon name="shield" class="text-muted" />
        <div class="min-w-0 flex-[1_1_180px]">
          <div class="truncate text-[14px] font-semibold">{t.name}</div>
          <div class="truncate text-[12px] text-muted">{scopeLabel(t.scope)}{#if t.hint} · <span class="font-mono">{t.hint}</span>{/if}</div>
        </div>
        <div class="text-[12px] text-muted md:w-44 md:text-right">
          Last used <span class="font-mono">{ago(t.lastUsed)}</span><br />
          {#if t.expiresAt}Expires <span class="font-mono">{shortDate(t.expiresAt)}</span>{:else}No expiry{/if}
        </div>
        <Button variant="ghost" onclick={() => ((revoking = t), (revokeError = ''), (revokeOpen = true))}>Revoke</Button>
      </li>
    {/each}
  </ul>
</div>

<Dialog bind:open={createOpen} title="New access token">
  <form
    id="token-form"
    class="flex flex-col gap-4"
    novalidate
    onsubmit={(e) => {
      e.preventDefault()
      create()
    }}
  >
    <FormError message={createMessage} />
    <Field label="Name" bind:value={name} error={fields.name} hint="For example: Claude on the laptop. Shown in the history of changes." required />
    <Select label="Access" bind:value={scope} options={[{ value: 'read', label: 'Read only' }, { value: 'write', label: 'Read and write' }]} />
    <Select label="Expires" bind:value={days} options={[{ value: '', label: 'Never' }, { value: '30', label: 'After 30 days' }, { value: '90', label: 'After 90 days' }, { value: '365', label: 'After 1 year' }]} />
  </form>
  {#snippet actions()}
    <Button onclick={() => (createOpen = false)}>Cancel</Button>
    <Button variant="primary" type="submit" form="token-form" disabled={busy}>{busy ? 'Creating' : 'Create token'}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={shownOpen} title="Copy your token">
  <div class="flex flex-col gap-4">
    <p class="m-0 text-[14px]"><strong>You won't see this again.</strong> Copy the token for "{shownName}" now and keep it somewhere safe. If you lose it, revoke it and create a new one.</p>
    <CopyField label="Token" value={shownToken} />
  </div>
  {#snippet actions()}<Button variant="primary" onclick={() => ((shownOpen = false), (shownToken = ''))}>Done</Button>{/snippet}
</Dialog>

<Dialog bind:open={revokeOpen} title="Revoke token">
  <div class="flex flex-col gap-3">
    <FormError message={revokeError} />
    <p class="m-0 text-[14px]">Revoke "{revoking?.name}"? Anything using it stops working at once. This cannot be undone.</p>
  </div>
  {#snippet actions()}
    <Button onclick={() => (revokeOpen = false)}>Cancel</Button>
    <Button variant="primary" onclick={revoke}>Revoke</Button>
  {/snippet}
</Dialog>

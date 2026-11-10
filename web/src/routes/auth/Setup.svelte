<script lang="ts">
  import { api } from '../../lib/api'
  import { auth, loadSession } from '../../lib/auth.svelte'
  import { explain, passwordProblem } from '../../lib/errors'
  import { browserLanguage } from '../../lib/format'
  import { navigate } from '../../lib/router.svelte'
  import AuthFrame from '../../lib/shell/AuthFrame.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'

  let name = $state('')
  let email = $state('')
  let password = $state('')
  let householdName = $state('')
  let setupToken = $state('')
  let fields = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    message = ''
    fields = {}
    const p = passwordProblem(password)
    if (p) return void (fields = { password: p })
    busy = true
    try {
      await api.post('/api/v1/setup', { name, email, password, household_name: householdName, language: browserLanguage(), ...(auth.setupToken ? { setup_token: setupToken } : {}) })
      await loadSession()
      navigate('/', true)
    } catch (err) {
      ;({ fields, message } = explain(err))
    } finally {
      busy = false
    }
  }
</script>

<AuthFrame icon="home" title="Set up" subtitle="Create the first account and the household it belongs to. You will be the owner.">
  <form onsubmit={submit} class="flex flex-col gap-4" novalidate>
    <FormError {message} />
    <Field label="Your name" bind:value={name} error={fields.name} autocomplete="name" required />
    <Field label="Email" type="email" bind:value={email} error={fields.email} autocomplete="email" required />
    <PasswordField label="Password" bind:value={password} error={fields.password} hint="At least 10 characters." autocomplete="new-password" />
    <Field label="Household name" bind:value={householdName} error={fields.household_name} autocomplete="off" required />
    {#if auth.setupToken}
      <Field label="Setup token" bind:value={setupToken} error={fields.setup_token} hint="Find it in your server's environment (INVENTORY_SETUP_TOKEN)" autocomplete="off" required />
    {/if}
    <Button variant="primary" type="submit" disabled={busy} class="w-full">{busy ? 'Setting up' : 'Create household'}</Button>
  </form>
</AuthFrame>

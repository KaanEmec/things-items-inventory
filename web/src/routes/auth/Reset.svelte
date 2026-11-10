<script lang="ts">
  import { api } from '../../lib/api'
  import { boot } from '../../lib/auth.svelte'
  import { explain, passwordProblem } from '../../lib/errors'
  import { navigate } from '../../lib/router.svelte'
  import AuthFrame from '../../lib/shell/AuthFrame.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'

  let { token }: { token: string } = $props()
  let password = $state('')
  let fields = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)
  let done = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    message = ''
    fields = {}
    const p = passwordProblem(password)
    if (p) return void (fields = { password: p })
    busy = true
    try {
      await api.post('/api/v1/auth/reset', { token, password })
      done = true
    } catch (err) {
      ;({ fields, message } = explain(err))
      // The token is a path segment, not a field: show its problem as a general message.
      if (fields.token) message = fields.token
    } finally {
      busy = false
    }
  }

  async function toSignIn() {
    navigate('/', true)
    await boot()
  }
</script>

<AuthFrame icon="shield" title="Choose a new password">
  {#if done}
    <p class="m-0 text-[14px]">Your password has been changed. Sign in with the new one.</p>
    <Button variant="primary" onclick={toSignIn} class="w-full">Sign in</Button>
  {:else}
    <form onsubmit={submit} class="flex flex-col gap-4" novalidate>
      <FormError {message} />
      <PasswordField label="New password" bind:value={password} error={fields.password} hint="At least 10 characters." autocomplete="new-password" />
      <Button variant="primary" type="submit" disabled={busy} class="w-full">{busy ? 'Saving' : 'Change password'}</Button>
    </form>
  {/if}
</AuthFrame>

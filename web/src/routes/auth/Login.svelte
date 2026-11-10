<script lang="ts">
  import { login, takeReturnTo } from '../../lib/auth.svelte'
  import { explain } from '../../lib/errors'
  import { navigate } from '../../lib/router.svelte'
  import AuthFrame from '../../lib/shell/AuthFrame.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'

  let email = $state('')
  let password = $state('')
  let fields = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    message = ''
    fields = {}
    busy = true
    try {
      await login(email, password)
      navigate(takeReturnTo(), true)
    } catch (err) {
      ;({ fields, message } = explain(err))
    } finally {
      busy = false
    }
  }
</script>

<AuthFrame icon="user" title="Sign in">
  <form onsubmit={submit} class="flex flex-col gap-4" novalidate>
    <FormError {message} />
    <Field label="Email" type="email" bind:value={email} error={fields.email} autocomplete="username" required />
    <PasswordField label="Password" bind:value={password} error={fields.password} autocomplete="current-password" />
    <Button variant="primary" type="submit" disabled={busy} class="w-full">{busy ? 'Signing in' : 'Sign in'}</Button>
  </form>
  {#snippet footer()}Forgot your password? Ask an owner or admin for a reset link.{/snippet}
</AuthFrame>

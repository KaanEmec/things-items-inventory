<script lang="ts">
  // One banner for the three things that stop the app from talking to the server:
  // no network, a server that is updating, and a session that ended. Words and icon, not colour alone.
  import { login } from '../auth.svelte'
  import { conn } from '../connection.svelte'
  import { explain } from '../errors'
  import Icon from '../Icon.svelte'
  import { pwa } from '../pwa/pwa.svelte'
  import Button from '../ui/Button.svelte'
  import Dialog from '../ui/Dialog.svelte'
  import Field from '../ui/Field.svelte'
  import FormError from '../ui/FormError.svelte'
  import PasswordField from '../ui/PasswordField.svelte'

  let open = $state(false)
  let email = $state('')
  let password = $state('')
  let message = $state('')
  let busy = $state(false)

  const kind = $derived(conn.sessionEnded ? 'session' : !pwa.online ? 'offline' : conn.serverUpdating ? 'updating' : null)
  const text = {
    session: 'Your session ended. Sign in again to save. What you typed is still here.',
    offline: "You're offline. Changes will not save until you're back.",
    updating: 'The server is updating. This usually takes a minute; try again shortly.',
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    message = ''
    busy = true
    try {
      await login(email, password)
      conn.sessionEnded = false
      password = ''
      open = false
    } catch (err) {
      message = explain(err).message
    } finally {
      busy = false
    }
  }
</script>

{#if kind}
  <div role={kind === 'session' ? 'alert' : 'status'} class="flex flex-none flex-wrap items-center gap-x-3 gap-y-1 border-b border-border bg-ink px-4 py-2 text-[13px] text-white">
    <Icon name={kind === 'updating' ? 'refresh' : 'alert'} size={16} />
    <span class="min-w-0 flex-1">{text[kind]}</span>
    {#if kind === 'session'}<Button variant="primary" onclick={() => (open = true)}>Sign in</Button>{/if}
  </div>
{/if}

<Dialog bind:open title="Sign in again">
  <form onsubmit={submit} class="flex flex-col gap-4" novalidate>
    <FormError {message} />
    <Field label="Email" type="email" bind:value={email} autocomplete="username" required />
    <PasswordField label="Password" bind:value={password} autocomplete="current-password" />
    <Button variant="primary" type="submit" disabled={busy} class="w-full">{busy ? 'Signing in' : 'Sign in'}</Button>
  </form>
</Dialog>

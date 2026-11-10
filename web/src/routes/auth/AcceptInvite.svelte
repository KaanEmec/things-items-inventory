<script lang="ts">
  import { ApiError, api } from '../../lib/api'
  import { auth, loadSession, requireLogin, roleLabels, type Role } from '../../lib/auth.svelte'
  import { explain, passwordProblem } from '../../lib/errors'
  import { navigate } from '../../lib/router.svelte'
  import AuthFrame from '../../lib/shell/AuthFrame.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'

  let { token }: { token: string } = $props()

  // GET /invites/{token}: { household_name, role, valid }
  type Invite = { household_name: string; role: Role; valid: boolean }
  let invite = $state<Invite | null>(null)
  let loadError = $state('')
  let name = $state('')
  let email = $state('')
  let password = $state('')
  let fields = $state<Record<string, string>>({})
  let message = $state('')
  let busy = $state(false)

  $effect(() => {
    invite = null
    loadError = ''
    api
      .get<Invite>(`/api/v1/invites/${encodeURIComponent(token)}`)
      .then((i) => (invite = i))
      .catch((e) => {
        if (e instanceof ApiError && e.status !== 0 && e.status < 500) invite = { household_name: '', role: 'member', valid: false }
        else loadError = e.message
      })
  })

  const signedIn = $derived(auth.status === 'ready')

  async function accept(e?: SubmitEvent) {
    e?.preventDefault()
    message = ''
    fields = {}
    if (!signedIn) {
      const p = passwordProblem(password)
      if (p) return void (fields = { password: p })
    }
    busy = true
    try {
      await api.post(`/api/v1/invites/${encodeURIComponent(token)}/accept`, signedIn ? {} : { name, email, password })
      await loadSession()
      navigate('/', true)
    } catch (err) {
      ;({ fields, message } = explain(err))
    } finally {
      busy = false
    }
  }

  function signIn() {
    requireLogin(location.pathname + location.hash)
    navigate('/login')
  }
</script>

<AuthFrame icon="home" title="Join a household">
  {#if loadError}
    <FormError message={loadError} />
  {:else if !invite}
    <p class="m-0 text-[14px] text-muted">Checking the invite link.</p>
  {:else if !invite.valid}
    <p class="m-0 text-[14px]">This invite link has expired or was already used. Ask the person who sent it for a new one.</p>
    <Button href="/" class="w-full">Go to the app</Button>
  {:else}
    <p class="m-0 text-[14px]">
      You have been invited to <strong>{invite.household_name}</strong> as {roleLabels[invite.role]?.toLowerCase() ?? invite.role}.
    </p>
    {#if signedIn}
      <p class="m-0 text-[13px] text-muted">You are signed in as {auth.user?.name}. The household will be added to your account.</p>
      <FormError {message} />
      <Button variant="primary" onclick={() => accept()} disabled={busy} class="w-full">{busy ? 'Joining' : 'Join household'}</Button>
    {:else}
      <form onsubmit={accept} class="flex flex-col gap-4" novalidate>
        <FormError {message} />
        <Field label="Your name" bind:value={name} error={fields.name} autocomplete="name" required />
        <Field label="Email" type="email" bind:value={email} error={fields.email} autocomplete="email" required />
        <PasswordField label="Password" bind:value={password} error={fields.password} hint="At least 10 characters." autocomplete="new-password" />
        <Button variant="primary" type="submit" disabled={busy} class="w-full">{busy ? 'Joining' : 'Create account and join'}</Button>
      </form>
    {/if}
  {/if}
  {#snippet footer()}
    {#if invite?.valid && !signedIn}Already have an account? <button type="button" onclick={signIn} class="cursor-pointer font-semibold text-accent">Sign in first</button>{:else}&nbsp;{/if}
  {/snippet}
</AuthFrame>

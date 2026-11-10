<script lang="ts">
  import { api } from '../../lib/api'
  import { auth, logout, logoutAll } from '../../lib/auth.svelte'
  import { explain, passwordProblem } from '../../lib/errors'
  import { navigate } from '../../lib/router.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import PasswordField from '../../lib/ui/PasswordField.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'
  import Block from './Block.svelte'

  let name = $state(auth.user?.name ?? '')
  let email = $state(auth.user?.email ?? '')
  let pf = $state<Record<string, string>>({})
  let pm = $state('')
  let current = $state('')
  let next = $state('')
  let nf = $state<Record<string, string>>({})
  let nm = $state('')
  let busy = $state(false)

  async function saveProfile(e: SubmitEvent) {
    e.preventDefault()
    pm = ''
    pf = {}
    busy = true
    try {
      await api.patch('/api/v1/me', { name, email })
      if (auth.user) Object.assign(auth.user, { name, email })
      showToast('Account saved', 'success')
    } catch (err) {
      ;({ fields: pf, message: pm } = explain(err))
    } finally {
      busy = false
    }
  }

  async function savePassword(e: SubmitEvent) {
    e.preventDefault()
    nm = ''
    nf = {}
    const p = passwordProblem(next)
    if (p) return void (nf = { new: p })
    busy = true
    try {
      await api.post('/api/v1/me/password', { current, new: next })
      current = next = ''
      showToast('Password changed', 'success')
    } catch (err) {
      ;({ fields: nf, message: nm } = explain(err))
    } finally {
      busy = false
    }
  }

  async function out(all: boolean) {
    try {
      await (all ? logoutAll() : logout())
      navigate('/', true)
    } catch (err) {
      showToast(explain(err).message, 'error')
    }
  }
</script>

<Block title="Profile">
  <form onsubmit={saveProfile} class="flex flex-col gap-4" novalidate>
    <FormError message={pm} />
    <Field label="Name" bind:value={name} error={pf.name} autocomplete="name" required />
    <Field label="Email" type="email" bind:value={email} error={pf.email} autocomplete="email" required />
    <div><Button variant="primary" type="submit" disabled={busy}>Save</Button></div>
  </form>
</Block>

<Block title="Password">
  <form onsubmit={savePassword} class="flex flex-col gap-4" novalidate>
    <FormError message={nm} />
    <PasswordField label="Current password" bind:value={current} error={nf.current} autocomplete="current-password" />
    <PasswordField label="New password" bind:value={next} error={nf.new} hint="At least 10 characters." autocomplete="new-password" />
    <div><Button variant="primary" type="submit" disabled={busy}>Change password</Button></div>
  </form>
</Block>

<Block title="Sessions" note="Logging out everywhere also ends this session.">
  <div class="flex flex-wrap gap-2">
    <Button icon="log-out" onclick={() => out(false)}>Log out</Button>
    <Button onclick={() => out(true)}>Log out everywhere</Button>
  </div>
</Block>

<script lang="ts">
  import { api } from '../../lib/api'
  import { atLeast, auth, can, roleLabels, roles, type Role } from '../../lib/auth.svelte'
  import { explain } from '../../lib/errors'
  import { ago, shortDate } from '../../lib/format'
  import Icon from '../../lib/Icon.svelte'
  import Avatar from '../../lib/ui/Avatar.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import Dialog from '../../lib/ui/Dialog.svelte'
  import FormError from '../../lib/ui/FormError.svelte'
  import Popover from '../../lib/ui/Popover.svelte'
  import Select from '../../lib/ui/Select.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'
  import LinkDialog from './LinkDialog.svelte'

  interface Member {
    id: string
    name: string
    email: string
    role: Role
    lastActive: string | null
  }
  interface Invite {
    id: string
    role: Role
    expiresAt: string
  }

  // GET /members: { members: [{user_id, name, email, role, last_active_at}], invites: [{id, role, expires_at}] }
  let members = $state<Member[]>([])
  let invites = $state<Invite[]>([])
  let loaded = $state(false)
  let message = $state('')
  const manage = $derived(can('admin'))
  const owners = $derived(members.filter((m) => m.role === 'owner').length)

  async function load() {
    try {
      const r = await api.get<any>('/api/v1/members')
      members = (r.members ?? []).map((m: any) => ({
        id: String(m.user_id ?? m.id),
        name: m.name,
        email: m.email,
        role: m.role,
        lastActive: m.last_active_at ?? m.last_active ?? null,
      }))
      invites = (r.invites ?? []).map((i: any) => ({ id: String(i.id), role: i.role, expiresAt: i.expires_at }))
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

  const absolute = (u: string) => (u.startsWith('/') ? location.origin + u : u)

  // Role
  async function setRole(m: Member, role: Role) {
    try {
      await api.patch(`/api/v1/members/${m.id}`, { role })
      showToast(`${m.name} is now ${roleLabels[role].toLowerCase()}`, 'success')
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
    await load()
  }

  // Remove
  let removing = $state<Member | null>(null)
  let removeOpen = $state(false)
  let removeError = $state('')
  async function remove() {
    if (!removing) return
    removeError = ''
    try {
      await api.del(`/api/v1/members/${removing.id}`)
      removeOpen = false
      showToast(`${removing.name} was removed`, 'success')
      await load()
    } catch (e) {
      removeError = explain(e).message
    }
  }

  // Links
  let linkOpen = $state(false)
  let link = $state({ title: '', url: '', note: '', qr: false })
  async function resetLink(m: Member) {
    try {
      const r = await api.post<{ url: string }>(`/api/v1/members/${m.id}/reset-link`)
      link = { title: 'Password reset link', url: absolute(r.url), note: `Send this link to ${m.name}. It works once and lets them choose a new password.`, qr: false }
      linkOpen = true
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
  }

  // Invite
  let inviteOpen = $state(false)
  let inviteRole = $state<Role>('member')
  let inviteDays = $state('7')
  let inviteError = $state('')
  const inviteRoles = $derived(roles.filter((r) => atLeast(auth.role, 'owner') || roles.indexOf(r) < roles.indexOf(auth.role ?? 'viewer')))
  async function createInvite() {
    inviteError = ''
    try {
      const r = await api.post<{ url: string; expires_at?: string }>('/api/v1/invites', { role: inviteRole, expires_in_days: Number(inviteDays) })
      inviteOpen = false
      const until = r.expires_at ? ` until ${shortDate(r.expires_at)}` : ''
      link = { title: 'Invite link', url: absolute(r.url), note: `Single use, valid${until || ` for ${inviteDays} days`}. The person joins as ${roleLabels[inviteRole].toLowerCase()}.`, qr: true }
      linkOpen = true
      await load()
    } catch (e) {
      inviteError = explain(e).message
    }
  }
  async function revoke(i: Invite) {
    try {
      await api.del(`/api/v1/invites/${i.id}`)
      showToast('Invite revoked', 'success')
    } catch (e) {
      showToast(explain(e).message, 'error')
    }
    await load()
  }

  // Admins manage members and viewers only; owners manage everyone.
  const locked = (m: Member) => !can('owner') && atLeast(m.role, auth.role ?? 'viewer')
  const reason = (m: Member) =>
    m.role === 'owner' && owners < 2 ? 'Last owner. Make someone else an owner first.' : locked(m) ? 'Only an owner can change an admin or owner.' : ''
  const roleBlocked = (r: Role) => !can('owner') && atLeast(r, auth.role ?? 'viewer')
  const selectCls = 'h-11 rounded-control border border-border-input bg-surface px-2 text-[14px] md:h-8 md:text-[13px] disabled:opacity-50'
</script>

<div class="border-b border-divider bg-surface max-md:border-t">
  <div class="flex min-h-12 items-center gap-2 border-b border-divider bg-surface-subtle px-4 md:px-6">
    <h3 class="m-0 flex-1 text-[13px] font-semibold">Members <span class="font-mono font-medium text-muted">{members.length}</span></h3>
    {#if manage}<Button variant="primary" icon="plus" onclick={() => (inviteOpen = true)}>Invite</Button>{/if}
  </div>
  {#if message}<div class="p-4 md:px-6"><FormError {message} /></div>{/if}
  {#if !loaded}
    <p class="m-0 px-4 py-4 text-[14px] text-muted md:px-6">Loading</p>
  {/if}
  <ul class="m-0 list-none divide-y divide-divider-row p-0">
    {#each members as m (m.id)}
      {@const why = reason(m)}
      {@const self = m.id === auth.user?.id}
      <li class="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3 md:px-6">
        <Avatar name={m.name} size={34} />
        <div class="min-w-0 flex-[1_1_160px]">
          <div class="truncate text-[14px] font-semibold">{m.name}{#if self}<span class="font-normal text-muted"> (you)</span>{/if}</div>
          <div class="truncate text-[12px] text-muted">{m.email}</div>
        </div>
        <div class="font-mono text-[12px] text-muted md:w-28">{ago(m.lastActive)}</div>
        {#if manage}
          <select
            class={selectCls}
            aria-label="Role of {m.name}"
            value={m.role}
            disabled={!!why}
            title={why || undefined}
            onchange={(e) => setRole(m, e.currentTarget.value as Role)}
          >
            {#each roles as r}<option value={r} disabled={roleBlocked(r) && r !== m.role}>{roleLabels[r]}</option>{/each}
          </select>
          <Popover align="right">
            {#snippet trigger({ open, toggle })}
              <button type="button" onclick={toggle} aria-expanded={open} aria-label="Actions for {m.name}" class="flex size-11 cursor-pointer items-center justify-center rounded-control text-muted hover:bg-chrome-hover md:size-9">
                <Icon name="more" size={18} />
              </button>
            {/snippet}
            <button type="button" disabled={locked(m)} onclick={() => resetLink(m)} class="flex disabled:cursor-not-allowed disabled:opacity-50 h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-[14px] hover:bg-chrome-hover md:h-9"><Icon name="link" class="text-muted" />Create reset link</button>
            <button
              type="button"
              disabled={!!why || self}
              onclick={() => ((removing = m), (removeError = ''), (removeOpen = true))}
              class="flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-[14px] text-danger hover:bg-chrome-hover disabled:cursor-not-allowed disabled:opacity-50 md:h-9"
            ><Icon name="trash" />Remove from household</button>
            {#if why || self}<p class="m-0 px-2.5 pt-1 pb-2 text-[12px] text-muted">{why || 'You cannot remove yourself.'}</p>{/if}
          </Popover>
        {:else}
          <span class="text-[13px] text-body md:w-20">{roleLabels[m.role]}</span>
        {/if}
      </li>
    {/each}
  </ul>
  {#if manage && invites.length}
    <div class="flex h-11 items-center border-y border-divider bg-surface-subtle px-4 text-[11px] font-semibold tracking-[0.08em] text-label uppercase md:px-6">Pending invites</div>
    <ul class="m-0 list-none divide-y divide-divider-row p-0">
      {#each invites as i (i.id)}
        <li class="flex items-center gap-3 px-4 py-2.5 md:px-6">
          <Icon name="link" class="text-muted" />
          <div class="min-w-0 flex-1 text-[14px]">{roleLabels[i.role]} invite <span class="text-muted">expires <span class="font-mono text-[12px]">{shortDate(i.expiresAt)}</span></span></div>
          <Button variant="ghost" onclick={() => revoke(i)}>Revoke</Button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
{#if !manage && loaded && !message}
  <p class="m-0 px-4 py-3 text-[13px] text-muted md:px-6">Only owners and admins can invite people or change roles.</p>
{/if}

<Dialog bind:open={inviteOpen} title="Invite someone">
  <div class="flex flex-col gap-4">
    <FormError message={inviteError} />
    <Select label="Role" bind:value={inviteRole} options={inviteRoles.map((r) => ({ value: r, label: roleLabels[r] }))} />
    <Select label="Expires after" bind:value={inviteDays} options={[{ value: '1', label: '1 day' }, { value: '7', label: '7 days' }, { value: '30', label: '30 days' }]} />
  </div>
  {#snippet actions()}
    <Button onclick={() => (inviteOpen = false)}>Cancel</Button>
    <Button variant="primary" onclick={createInvite}>Create link</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={removeOpen} title="Remove member">
  <div class="flex flex-col gap-3">
    <FormError message={removeError} />
    <p class="m-0 text-[14px]">Remove {removing?.name} from this household? They lose access to it. Items stay.</p>
  </div>
  {#snippet actions()}
    <Button onclick={() => (removeOpen = false)}>Cancel</Button>
    <Button variant="primary" onclick={remove}>Remove</Button>
  {/snippet}
</Dialog>

<LinkDialog bind:open={linkOpen} title={link.title} url={link.url} note={link.note} qr={link.qr} />

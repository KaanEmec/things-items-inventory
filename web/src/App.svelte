<script lang="ts">
  import { onMount } from 'svelte'
  import Shell from './lib/shell/Shell.svelte'
  import { auth, boot } from './lib/auth.svelte'
  import { captureQueue } from './lib/capture/queue.svelte'
  import './lib/stream.svelte' // opens the live stream while signed in
  import { fragmentToken, navigate, router, startRouter } from './lib/router.svelte'
  import Button from './lib/ui/Button.svelte'
  import Toast from './lib/ui/Toast.svelte'
  import AcceptInvite from './routes/auth/AcceptInvite.svelte'
  import Login from './routes/auth/Login.svelte'
  import Reset from './routes/auth/Reset.svelte'
  import Setup from './routes/auth/Setup.svelte'
  import NotFound from './routes/NotFound.svelte'
  import { findRoute } from './routes'

  onMount(() => {
    boot()
    return startRouter()
  })

  const Page = $derived(findRoute(router.path)?.component ?? NotFound)
  // One-time links: /reset#token=... and /invite#token=... (the token stays in the fragment).
  const resetToken = $derived(router.path === '/reset' ? fragmentToken(router.hash) : '')
  const inviteToken = $derived(router.path === '/invite' ? fragmentToken(router.hash) : '')

  // Photos waiting from an earlier visit start uploading as soon as someone is signed in.
  $effect(() => {
    if (auth.status !== 'ready') return
    void captureQueue.start()
    return captureQueue.attach()
  })

  // Signed in people have no use for the sign-in or setup addresses.
  $effect(() => {
    if (auth.status === 'ready' && (router.path === '/login' || router.path === '/setup')) navigate('/', true)
  })
</script>

{#if auth.status === 'loading'}
  <div class="flex min-h-dvh items-center justify-center bg-chrome text-[14px] text-muted" role="status">Loading</div>
{:else if auth.status === 'error'}
  <div class="flex min-h-dvh flex-col items-center justify-center gap-3 bg-chrome p-6 text-center">
    <p class="m-0 text-[14px]" role="alert">{auth.error}</p>
    <Button onclick={boot}>Try again</Button>
  </div>
{:else if auth.status === 'setup'}
  <Setup />
{:else if resetToken}
  <Reset token={resetToken} />
{:else if inviteToken}
  {#key inviteToken}<AcceptInvite token={inviteToken} />{/key}
{:else if auth.status === 'login'}
  <Login />
{:else}
  <!-- Remount the page when the active household changes. -->
  {#key auth.householdId}
    <Shell><Page /></Shell>
  {/key}
{/if}
<Toast />

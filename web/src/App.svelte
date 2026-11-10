<script lang="ts">
  import { onMount } from 'svelte'
  import Shell from './lib/shell/Shell.svelte'
  import { router, startRouter } from './lib/router.svelte'
  import Toast from './lib/ui/Toast.svelte'
  import NotFound from './routes/NotFound.svelte'
  import { routes } from './routes'

  onMount(startRouter)
  const Page = $derived(routes.find((r) => r.path === router.path)?.component ?? NotFound)
</script>

<Shell><Page /></Shell>
<Toast />

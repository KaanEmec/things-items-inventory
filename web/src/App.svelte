<script lang="ts">
  import { APP_NAME } from './lib/app'

  const health = fetch('/api/health').then((r) => r.json())
</script>

<main class="mx-auto max-w-xl p-8">
  <h1 class="text-3xl font-semibold">{APP_NAME}</h1>
  {#await health}
    <p class="mt-4 text-gray-500">Checking server…</p>
  {:then h}
    <p class="mt-4 text-gray-700">Server {h.version}, built {h.buildTime}</p>
  {:catch}
    <p class="mt-4 text-red-600">Server unreachable</p>
  {/await}
</main>

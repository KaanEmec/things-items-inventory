<script lang="ts">
  import type { Snippet } from 'svelte'

  let { children, class: cls = '' }: { children: Snippet; class?: string } = $props()
</script>

<!-- The one white workspace panel. Stack PageHeader, Toolbar, Body/Split, PanelFooter inside. -->
<main
  class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden bg-surface md:m-2 md:ml-0 md:rounded-panel md:border md:border-border md:shadow-panel {cls}"
>
  {@render children()}
</main>

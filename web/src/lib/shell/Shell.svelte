<script lang="ts">
  import type { Snippet } from 'svelte'
  import SideNav from './SideNav.svelte'
  import TabBar from './TabBar.svelte'

  let { children }: { children: Snippet } = $props()
</script>

<!-- Desktop: tinted chrome with side nav; the page supplies the one white Panel.
     Phone: the Panel fills the screen above the tab bar. -->
<div class="flex h-dvh flex-col bg-phone-bg md:flex-row md:bg-chrome">
  <SideNav />
  {@render children()}
  <TabBar />
</div>

<script lang="ts">
  import type { Snippet } from 'svelte'
  import { navigate } from '../router.svelte'
  import { canFireShortcut, dialogIsOpen, isPaletteKey } from '../search/guard'
  import Palette from '../search/Palette.svelte'
  import { palette } from '../search/state.svelte'
  import SideNav from './SideNav.svelte'
  import TabBar from './TabBar.svelte'

  let { children }: { children: Snippet } = $props()

  function onkeydown(e: KeyboardEvent) {
    if (e.isComposing) return
    if (isPaletteKey(e)) {
      // Works while typing; another dialog (not the palette) blocks it.
      if (dialogIsOpen() && !palette.open) return
      e.preventDefault()
      palette.toggle()
    } else if ((e.key === 'c' || e.key === 'C') && canFireShortcut(e, dialogIsOpen())) {
      e.preventDefault()
      navigate('/capture')
    }
  }
</script>

<svelte:window {onkeydown} />

<!-- Desktop: tinted chrome with side nav; the page supplies the one white Panel.
     Phone: the Panel fills the screen above the tab bar. -->
<div class="flex h-dvh flex-col bg-phone-bg md:flex-row md:bg-chrome">
  <SideNav />
  {@render children()}
  <TabBar />
</div>

<Palette />

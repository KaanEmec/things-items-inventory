<script lang="ts">
  import type { Snippet } from 'svelte'
  import { APP_NAME } from '../app'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'

  interface Props {
    icon: IconName
    title: string
    subtitle?: string
    children: Snippet
    /** Docked footer band under the form. */
    footer?: Snippet
  }
  let { icon, title, subtitle, children, footer }: Props = $props()
</script>

<!-- Screens outside the app shell: one small docked white panel in the tinted chrome.
     On phone the panel fills the screen. -->
<div class="flex min-h-dvh items-center justify-center bg-phone-bg md:bg-chrome md:p-4">
  <main class="flex w-full flex-col overflow-hidden bg-surface max-md:min-h-dvh md:max-w-[420px] md:rounded-panel md:border md:border-border md:shadow-panel">
    <header class="flex items-center gap-3 border-b border-divider px-4 py-3.5 max-md:pt-[calc(env(safe-area-inset-top)+14px)] md:px-6">
      <span class="flex size-[38px] flex-none items-center justify-center rounded-tile bg-accent-soft text-accent"><Icon name={icon} size={20} /></span>
      <div class="min-w-0">
        <div class="text-[12px] font-semibold text-muted">{APP_NAME}</div>
        <h1 class="m-0 text-[18px] font-bold tracking-[-0.01em]">{title}</h1>
      </div>
    </header>
    <div class="flex flex-1 flex-col gap-4 px-4 py-5 md:px-6">
      {#if subtitle}<p class="m-0 text-[14px] text-muted">{subtitle}</p>{/if}
      {@render children()}
    </div>
    {#if footer}
      <footer class="flex flex-wrap items-center gap-x-3 border-t border-divider bg-surface-subtle px-4 py-3 text-[13px] text-muted max-md:pb-[max(env(safe-area-inset-bottom),12px)] md:px-6">
        {@render footer()}
      </footer>
    {/if}
  </main>
</div>

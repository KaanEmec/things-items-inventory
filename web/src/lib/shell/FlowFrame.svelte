<script lang="ts">
  import type { Snippet } from 'svelte'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'
  import { navigate } from '../router.svelte'
  import Body from './Body.svelte'
  import Panel from './Panel.svelte'
  import PageHeader from './PageHeader.svelte'

  interface Props {
    icon: IconName
    title: string
    breadcrumb?: string[]
    /** Desktop header actions. */
    actions?: Snippet
    /** Phone bottom action bar. */
    bar?: Snippet
    children?: Snippet
  }
  let { icon, title, breadcrumb, actions, bar, children }: Props = $props()

  const close = () => (history.length > 1 ? history.back() : navigate('/'))
</script>

<!-- Phone: full-screen over the tab bar with its own close and bottom action bar.
     Desktop: an ordinary panel with the standard page header. -->
<Panel class="max-md:fixed max-md:inset-0 max-md:z-40">
  <PageHeader {icon} {title} {breadcrumb} {actions} class="max-md:hidden" />
  <div class="flex h-14 flex-none items-center gap-1 border-b border-divider pr-4 pl-2 pt-[env(safe-area-inset-top)] box-content md:hidden">
    <button type="button" onclick={close} aria-label="Close" class="flex size-11 cursor-pointer items-center justify-center rounded-control text-body hover:bg-chrome-hover">
      <Icon name="x" size={20} />
    </button>
    <h1 class="m-0 text-[17px] font-bold">{title}</h1>
  </div>
  <Body>{@render children?.()}</Body>
  {#if bar}
    <div class="flex flex-none gap-2 border-t border-divider bg-surface px-4 pt-3 pb-[max(env(safe-area-inset-bottom),12px)] md:hidden">
      {@render bar()}
    </div>
  {/if}
</Panel>

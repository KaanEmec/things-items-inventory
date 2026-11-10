<script lang="ts">
  import { APP_NAME } from '../app'
  import { auth } from '../auth.svelte'
  import Icon from '../Icon.svelte'
  import Avatar from '../ui/Avatar.svelte'
  import HouseholdSwitcher from './HouseholdSwitcher.svelte'
  import { isActive } from '../router.svelte'
  import { routes } from '../../routes'

  const groups = [
    { label: 'Library', items: routes.filter((r) => r.side === 'library') },
    { label: 'Tools', items: routes.filter((r) => r.side === 'tools') },
  ]
  const settings = routes.find((r) => r.side === 'foot')!

  const item =
    'flex h-9 items-center gap-2.5 rounded-control px-2.5 text-[14px] font-medium text-body hover:bg-chrome-hover hover:text-ink aria-[current=page]:bg-surface aria-[current=page]:text-ink aria-[current=page]:shadow-[0_0_0_1px_var(--color-border),0_1px_2px_rgba(0,0,0,0.04)] [&>svg]:text-label aria-[current=page]:[&>svg]:text-accent'
</script>

<nav aria-label="Main" class="hidden w-56 flex-none flex-col gap-0.5 px-3 py-3.5 md:flex">
  <div class="mb-3 flex h-10 items-center gap-2.5 px-1.5">
    <span class="relative flex size-[30px] items-center justify-center rounded-[9px] bg-ink text-white">
      <Icon name="box" />
      <span class="absolute -top-[3px] -right-[3px] size-2.5 rounded-full border-2 border-chrome bg-accent"></span>
    </span>
    <span class="text-[16px] font-extrabold tracking-[-0.02em]">{APP_NAME}</span>
  </div>

  <a
    href="/capture"
    class="mb-2.5 flex h-[38px] items-center gap-2 rounded-[9px] bg-accent pr-2.5 pl-3 text-[14px] font-semibold text-white shadow-primary hover:bg-accent-hover"
  >
    <Icon name="camera" />Capture
    <kbd class="ml-auto inline-flex h-5 min-w-5 items-center justify-center rounded-[5px] bg-white/20 px-[5px] font-mono text-[11px] font-medium">C</kbd>
  </a>

  {#each groups as g (g.label)}
    <div class="px-2.5 pt-3 pb-1 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">{g.label}</div>
    {#each g.items as r (r.path)}
      <a href={r.path} class={item} aria-current={isActive(r.path) ? 'page' : undefined}>
        <Icon name={r.icon} />{r.title}
      </a>
    {/each}
  {/each}

  <div class="mt-auto flex flex-col gap-1 pt-4">
    <HouseholdSwitcher up class="mb-1" />
    <a href={settings.path} class={item} aria-current={isActive(settings.path) ? 'page' : undefined}>
      <Icon name={settings.icon} />{settings.title}
    </a>
    {#if auth.user}
      <a href="/settings/account" class="flex h-10 items-center gap-2.5 rounded-control px-2.5 hover:bg-chrome-hover">
        <Avatar name={auth.user.name} size={24} />
        <span class="min-w-0 truncate text-[13px] font-medium">{auth.user.name}</span>
      </a>
    {/if}
  </div>
</nav>

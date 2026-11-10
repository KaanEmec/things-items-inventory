<script lang="ts">
  import Icon from '../lib/Icon.svelte'
  import { router } from '../lib/router.svelte'
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import { visibleSections } from './settings/sections'

  const list = $derived(visibleSections())
  const selectedId = $derived(router.path.startsWith('/settings/') ? router.path.split('/')[2] : '')
  const selected = $derived(list.find((s) => s.id === selectedId))
  // Desktop always shows a section; phone shows the list until one is picked.
  const active = $derived(selected ?? list[0])
  const Section = $derived(active?.component)

  const row =
    'flex min-h-11 items-center gap-2.5 px-4 text-[14px] font-medium text-body hover:bg-chrome-hover md:h-9 md:min-h-0 md:rounded-control md:px-2.5 md:aria-[current=page]:bg-accent-soft md:aria-[current=page]:text-accent'
</script>

<Panel>
  <PageHeader icon="sliders" title={selected ? selected.title : 'Settings'} breadcrumb={selected ? ['Settings'] : undefined} />
  <Body>
    <div class="flex min-h-full md:h-full">
      <nav
        aria-label="Settings sections"
        class="w-full flex-none divide-y divide-divider-row border-y border-divider bg-surface md:block md:w-56 md:divide-y-0 md:border-y-0 md:border-r md:bg-surface-subtle md:p-3 md:space-y-0.5 {selected ? 'max-md:hidden' : ''}"
      >
        {#each list as s (s.id)}
          <a href="/settings/{s.id}" class="{row} max-md:py-2.5" aria-current={active?.id === s.id ? 'page' : undefined}>
            <Icon name={s.icon} class="text-label md:text-inherit" />
            <span class="min-w-0 flex-1">
              {s.title}
              <span class="block text-[12px] font-normal text-muted md:hidden">{s.summary}</span>
            </span>
            <Icon name="chevron-right" size={14} class="text-faint md:hidden" />
          </a>
        {/each}
      </nav>
      <section class="min-w-0 flex-1 md:overflow-auto {selected ? '' : 'max-md:hidden'}" aria-label={active?.title}>
        <a href="/settings" class="flex h-11 items-center gap-1 border-b border-divider bg-surface px-3 text-[13px] font-semibold text-accent md:hidden">
          <Icon name="chevron-left" />Settings
        </a>
        {#if Section}{#key active.id}<Section />{/key}{/if}
      </section>
    </div>
  </Body>
</Panel>

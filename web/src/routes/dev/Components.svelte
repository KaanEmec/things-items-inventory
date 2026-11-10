<script lang="ts">
  import Icon from '../../lib/Icon.svelte'
  import { icons, type IconName } from '../../lib/icons'
  import Body from '../../lib/shell/Body.svelte'
  import Group from '../../lib/shell/Group.svelte'
  import PageHeader from '../../lib/shell/PageHeader.svelte'
  import Panel from '../../lib/shell/Panel.svelte'
  import PanelFooter from '../../lib/shell/PanelFooter.svelte'
  import SectionBar from '../../lib/shell/SectionBar.svelte'
  import Split from '../../lib/shell/Split.svelte'
  import Toolbar from '../../lib/shell/Toolbar.svelte'
  import Avatar from '../../lib/ui/Avatar.svelte'
  import Button from '../../lib/ui/Button.svelte'
  import CategoryTile, { categoryKeys } from '../../lib/ui/CategoryTile.svelte'
  import Dialog from '../../lib/ui/Dialog.svelte'
  import Field from '../../lib/ui/Field.svelte'
  import Popover from '../../lib/ui/Popover.svelte'
  import Select from '../../lib/ui/Select.svelte'
  import StatusPill, { statusKeys } from '../../lib/ui/StatusPill.svelte'
  import { showToast } from '../../lib/ui/toasts.svelte'

  let dialogOpen = $state(false)
  let name = $state('')
  let model = $state('MX Master 3S')
  let room = $state('office')
  const iconNames = Object.keys(icons) as IconName[]
  const h = 'text-[11px] font-semibold tracking-[0.08em] text-label uppercase'
</script>

<Panel>
  <PageHeader icon="sliders" title="Components" breadcrumb={['Dev']}>
    {#snippet actions()}<Button variant="primary" icon="plus">Primary</Button>{/snippet}
  </PageHeader>
  <Toolbar>
    <span class="text-[13px] text-muted">Toolbar band</span>
    <Button variant="ghost" icon="search">Ghost</Button>
  </Toolbar>
  <Body>
    <Split>
      {#snippet main()}
        <SectionBar icon="tag" title="Controls">{#snippet children()}<a href="/" class="text-accent">Overview</a>{/snippet}</SectionBar>
        <div class="flex flex-col gap-6 p-4 md:p-6">
          <section class="flex flex-col gap-3">
            <h3 class={h}>Buttons</h3>
            <div class="flex flex-wrap gap-2">
              <Button variant="primary" icon="camera">Capture</Button>
              <Button icon="plus">Quick add</Button>
              <Button variant="ghost">Ghost</Button>
              <Button href="/items" icon="arrow-right">Link</Button>
              <Button disabled>Disabled</Button>
            </div>
          </section>
          <section class="grid gap-4 sm:grid-cols-2">
            <Field label="Name" placeholder="Item name" bind:value={name} hint="Shown in lists" />
            <Field label="Model" ai bind:value={model} />
            <Field label="Serial" value="x" error="Serial is required" />
            <Select label="Room" bind:value={room} options={[{ value: 'office', label: 'Office' }, { value: 'kitchen', label: 'Kitchen' }]} />
          </section>
          <section class="flex flex-col gap-3">
            <h3 class={h}>Status pills</h3>
            <div class="flex flex-wrap gap-2">{#each statusKeys as s}<StatusPill status={s} />{/each}</div>
          </section>
          <section class="flex flex-col gap-3">
            <h3 class={h}>Category tiles and avatars</h3>
            <div class="flex flex-wrap items-center gap-3">
              {#each categoryKeys as c}<CategoryTile category={c} />{/each}
              <CategoryTile category="gadget" icon="drone" size={38} />
              <Avatar name="Kaan" /><Avatar name="Elif" /><Avatar name="Old Home" size={30} />
            </div>
          </section>
          <section class="flex flex-wrap items-start gap-2">
            <Popover>
              {#snippet trigger({ open, toggle })}
                <Button icon="more" aria-expanded={open} onclick={toggle}>Popover</Button>
              {/snippet}
              <button type="button" class="flex h-9 w-full items-center gap-2 rounded-control px-2.5 text-left hover:bg-chrome-hover"><Icon name="edit" />Edit</button>
              <button type="button" class="flex h-9 w-full items-center gap-2 rounded-control px-2.5 text-left hover:bg-chrome-hover"><Icon name="move" />Move</button>
            </Popover>
            <Button onclick={() => (dialogOpen = true)}>Dialog</Button>
            <Button onclick={() => showToast('Saved', 'success')}>Toast</Button>
          </section>
        </div>
      {/snippet}
      {#snippet side()}
        <SectionBar icon="clock" title="Grouped list" />
        <Group title="Recently touched">
          <a href="/items" class="flex min-h-[60px] items-center gap-3 px-4 py-2 md:px-6">
            <CategoryTile category="gadget" icon="drone" size={38} />
            <span class="min-w-0 flex-1"><b class="block text-[15px] font-semibold">DJI Mini 4 Pro</b><small class="text-[13px] text-muted">Red plastic box</small></span>
            <Avatar name="Kaan" />
          </a>
          <a href="/items" class="flex min-h-[60px] items-center gap-3 px-4 py-2 md:px-6">
            <CategoryTile category="clothing" size={38} />
            <span class="min-w-0 flex-1"><b class="block text-[15px] font-semibold">Blue rain jacket</b><small class="text-[13px] text-muted">Hanging rail</small></span>
            <StatusPill status="in-use" />
          </a>
        </Group>
      {/snippet}
    </Split>
    <SectionBar icon="image" title="Icons ({iconNames.length})" />
    <ul class="m-0 grid list-none grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-px p-4 md:px-6">
      {#each iconNames as n}
        <li class="flex flex-col items-center gap-1.5 py-3 text-[11px] text-muted"><Icon name={n} size={22} class="text-ink" />{n}</li>
      {/each}
    </ul>
  </Body>
  <PanelFooter><span>Panel footer</span><span class="ml-auto font-mono">v0.1</span></PanelFooter>
</Panel>

<Dialog bind:open={dialogOpen} title="Move item">
  <p class="m-0 text-body">Native dialog with focus trap and Escape to close.</p>
  {#snippet actions()}
    <Button onclick={() => (dialogOpen = false)}>Cancel</Button>
    <Button variant="primary" onclick={() => (dialogOpen = false)}>Move</Button>
  {/snippet}
</Dialog>

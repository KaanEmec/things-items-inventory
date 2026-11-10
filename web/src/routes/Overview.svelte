<script lang="ts">
  import { activeHousehold, can } from '../lib/auth.svelte'
  import { explain } from '../lib/errors'
  import { getOverview } from '../lib/overview/api'
  import FirstDay from '../lib/overview/FirstDay.svelte'
  import { isFirstDay } from '../lib/overview/logic'
  import MovingBand from '../lib/overview/MovingBand.svelte'
  import NeedsLook from '../lib/overview/NeedsLook.svelte'
  import RecentList from '../lib/overview/RecentList.svelte'
  import StatusStrip from '../lib/overview/StatusStrip.svelte'
  import type { Overview } from '../lib/overview/types'
  import SearchField from '../lib/search/SearchField.svelte'
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import SectionBar from '../lib/shell/SectionBar.svelte'
  import Button from '../lib/ui/Button.svelte'
  import FormError from '../lib/ui/FormError.svelte'

  let data = $state<Overview | null>(null)
  let error = $state('')

  async function load() {
    error = ''
    try {
      data = await getOverview()
    } catch (e) {
      error = explain(e).message
    }
  }
  $effect(() => {
    void load()
  })

  const canWrite = $derived(can('member'))
  const firstDay = $derived(data ? isFirstDay(data.counts, data.places) : false)
  const needsTotal = $derived(data ? data.needs_look.reduce((n, g) => n + g.count, 0) : 0)
</script>

<Panel>
  <PageHeader icon="dashboard" title="Overview" breadcrumb={activeHousehold() ? [activeHousehold()!.name] : undefined}>
    {#snippet actions()}
      <Button href="/quick-add" icon="table" class="max-md:hidden">Quick add</Button>
      <Button variant="primary" href="/capture" icon="camera" class="max-md:hidden">Capture</Button>
    {/snippet}
  </PageHeader>

  <Body>
    <div class="border-b border-divider bg-surface px-4 py-3 md:px-6">
      <SearchField placeholder="Search items, places and boxes…" class="md:max-w-[520px]" />
    </div>

    {#if error}
      <div class="bg-surface p-4 md:px-6">
        <FormError message={error} />
        <Button class="mt-3" icon="refresh" onclick={load}>Try again</Button>
      </div>
    {:else if !data}
      <p class="m-0 bg-surface px-4 py-6 text-[14px] text-muted md:px-6">Loading</p>
    {:else if firstDay}
      <FirstDay {canWrite} hasPlaces={data.places > 0} />
    {:else}
      <StatusStrip counts={data.counts} />
      {#if data.moving}<MovingBand moving={data.moving} />{/if}
      <div class="md:grid md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]">
        <section aria-label="Recently touched" class="max-md:mt-3 max-md:border-y max-md:border-divider md:border-r md:border-divider">
          <SectionBar icon="clock" title="Recently touched">
            <a href="/items" class="font-medium text-accent hover:underline">All items</a>
          </SectionBar>
          <div class="bg-surface"><RecentList entries={data.recent} /></div>
        </section>
        <section aria-label="Needs a look" class="max-md:mt-3 max-md:border-y max-md:border-divider">
          <SectionBar icon="bell" title="Needs a look">
            {#if needsTotal}<span class="font-mono text-muted">{needsTotal}</span>{/if}
          </SectionBar>
          <div class="bg-surface"><NeedsLook groups={data.needs_look} /></div>
        </section>
      </div>
    {/if}
  </Body>
</Panel>

<script lang="ts">
  import Body from '../lib/shell/Body.svelte'
  import PageHeader from '../lib/shell/PageHeader.svelte'
  import Panel from '../lib/shell/Panel.svelte'
  import SearchField from '../lib/search/SearchField.svelte'
</script>

<Panel>
  <PageHeader icon="dashboard" title="Overview" />
  <Body>
    <div class="p-4 md:hidden"><SearchField placeholder="Search items and places…" /></div>
  </Body>
</Panel>

<script lang="ts">
  import { navigate } from '../router.svelte'
  import Icon from '../Icon.svelte'
  import { packing } from './packing.svelte'

  interface Props {
    /** Label of the action; Capture says "Add to box", Quick add "Add to box" too. */
    action?: string
    onadd?: () => void
  }
  let { action = 'Add to box', onadd }: Props = $props()
</script>

{#if packing.box}
  <div class="flex flex-none items-center gap-3 border-t border-divider bg-surface px-4 py-2" role="status">
    <Icon name="box" size={18} class="text-accent" />
    <button type="button" class="min-w-0 flex-1 cursor-pointer truncate text-left text-[14px] font-bold" onclick={() => navigate('/moving')}>
      Packing box {packing.box.box_number}
    </button>
    {#if onadd}
      <button type="button" class="h-11 cursor-pointer rounded-control bg-accent px-4 text-[14px] font-semibold text-white" onclick={onadd}>{action}</button>
    {/if}
  </div>
{/if}

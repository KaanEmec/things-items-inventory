<script lang="ts">
  import { auth, switchHousehold } from '../auth.svelte'
  import Icon from '../Icon.svelte'
  import { navigate } from '../router.svelte'
  import Popover from '../ui/Popover.svelte'
  import { showToast } from '../ui/toasts.svelte'

  interface Props {
    class?: string
    align?: 'left' | 'right'
    up?: boolean
  }
  let { class: cls = '', align = 'left', up = false }: Props = $props()
  let open = $state(false)
  const current = $derived(auth.households.find((h) => h.id === auth.householdId))

  async function pick(id: string) {
    open = false
    if (id === auth.householdId) return
    try {
      await switchHousehold(id)
      navigate('/')
    } catch (e) {
      showToast(e instanceof Error ? e.message : 'Could not switch household.', 'error')
    }
  }
</script>

<!-- Only for people who belong to more than one household. -->
{#if auth.households.length > 1}
  <div class={cls}>
    <Popover bind:open {align} {up}>
      {#snippet trigger({ open, toggle })}
        <button
          type="button"
          onclick={toggle}
          aria-expanded={open}
          aria-haspopup="true"
          class="flex h-11 max-w-full cursor-pointer items-center gap-2 rounded-control border border-border-input bg-surface px-3 text-[13px] font-semibold text-ink hover:bg-surface-subtle md:h-9"
        >
          <Icon name="home" class="text-muted" />
          <span class="max-w-40 truncate">{current?.name ?? 'Household'}</span>
          <Icon name="chevron-down" size={14} class="text-muted" />
        </button>
      {/snippet}
      <div class="px-2.5 pt-1.5 pb-1 text-[11px] font-semibold tracking-[0.08em] text-label uppercase">Household</div>
      {#each auth.households as h (h.id)}
        <button
          type="button"
          onclick={() => pick(h.id)}
          class="flex h-11 w-full cursor-pointer items-center gap-2 rounded-control px-2.5 text-left text-[14px] hover:bg-chrome-hover md:h-9"
        >
          <span class="min-w-0 flex-1 truncate">{h.name}</span>
          {#if h.id === auth.householdId}<Icon name="check" class="text-accent" />{/if}
        </button>
      {/each}
    </Popover>
  </div>
{/if}

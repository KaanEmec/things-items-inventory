<script lang="ts">
  // The one place every AI control asks "can I use AI?". Put it next to (or instead of) an AI control.
  //   not set: nothing for members, a quiet link for owners and admins
  //   failing: a small warning linking to Settings
  //   paused:  "AI is paused"
  import { ai, type AIRole } from './ai.svelte'
  import { can } from './auth.svelte'
  import Icon from './Icon.svelte'

  let { role = 'vision' }: { role?: AIRole } = $props()
  $effect(() => {
    void ai.ensure()
  })
  const status = $derived(ai.loaded ? ai.status(role) : 'working')
</script>

{#if status === 'paused'}
  <p class="m-0 flex items-center gap-1.5 text-[12px] text-muted"><Icon name="clock" size={12} />AI is paused</p>
{:else if status === 'failing'}
  <p class="m-0 flex items-center gap-1.5 text-[12px] text-repair-fg">
    <Icon name="x" size={12} />AI is not working right now. <a class="underline" href="/settings/ai">Check the AI settings</a>
  </p>
{:else if status === 'not_set' && can('admin')}
  <p class="m-0 text-[12px] text-muted"><a class="underline" href="/settings/ai">Set up AI in Settings</a></p>
{/if}

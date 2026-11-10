<script lang="ts">
  import { ai } from '../../lib/ai.svelte'
  import { explain } from '../../lib/errors'
  import { showToast } from '../../lib/ui/toasts.svelte'

  let { editable }: { editable: boolean } = $props()
  let busy = $state(false)

  async function toggle(e: Event) {
    const box = e.currentTarget as HTMLInputElement
    const want = box.checked
    busy = true
    try {
      await ai.setPaused(want)
      showToast(want ? 'AI paused. New AI work waits in the queue.' : 'AI resumed', 'success')
    } catch (err) {
      showToast(explain(err).message, 'error')
    } finally {
      box.checked = ai.paused
      busy = false
    }
  }
</script>

<section class="border-b border-divider bg-surface max-md:mb-3 max-md:border-t" aria-label="Pause AI">
  <div class="flex flex-col gap-1 px-4 py-4 md:max-w-2xl md:px-6">
    {#if editable}
      <label class="flex min-h-11 cursor-pointer items-center gap-3 text-[14px] font-medium">
        <input type="checkbox" role="switch" checked={ai.paused} disabled={busy} onchange={toggle} class="size-5 accent-accent" />
        Pause AI
      </label>
    {:else}
      <div class="text-[14px] font-medium">{ai.paused ? 'AI is paused' : 'AI is on'}</div>
    {/if}
    <p class="m-0 text-[13px] text-muted">
      {ai.paused ? 'New AI work waits in the queue and starts again when AI is resumed. Nothing is lost.' : 'While paused, new AI work waits in the queue instead of failing.'}
      {#if !editable}An owner or admin can change this.{/if}
    </p>
  </div>
</section>

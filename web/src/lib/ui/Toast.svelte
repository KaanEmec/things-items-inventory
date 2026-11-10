<script lang="ts">
  import Icon from '../Icon.svelte'
  import { dismissToast, toasts } from './toasts.svelte'
</script>

<!-- Mount once, in App.svelte. Trigger with showToast(). -->
<div
  role="status"
  aria-live="polite"
  class="pointer-events-none fixed inset-x-4 bottom-[calc(env(safe-area-inset-bottom)+96px)] z-50 flex flex-col items-center gap-2 md:bottom-6"
>
  {#each toasts as t (t.id)}
    <div class="pointer-events-auto flex min-h-11 max-w-[420px] items-center gap-2.5 rounded-[10px] bg-ink px-4 text-[14px] font-medium text-white shadow-popover">
      <Icon name={t.kind === 'success' ? 'check' : 'bell'} />
      {t.message}
      {#if t.action}
        {@const run = t.action.run}
        <button type="button" class="-mr-2 ml-1 h-11 cursor-pointer rounded-control px-3 text-[14px] font-bold text-[#9db4ff] hover:bg-white/10" onclick={() => (dismissToast(t.id), run())}>{t.action.label}</button>
      {/if}
    </div>
  {/each}
</div>

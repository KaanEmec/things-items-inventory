<script lang="ts">
  import Icon from '../Icon.svelte'
  import { toasts } from './toasts.svelte'
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
    </div>
  {/each}
</div>

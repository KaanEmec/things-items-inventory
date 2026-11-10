<script lang="ts">
  import { ai } from '../../lib/ai.svelte'
  import { can } from '../../lib/auth.svelte'
  import { explain } from '../../lib/errors'
  import FormError from '../../lib/ui/FormError.svelte'
  import AiBand from './AiBand.svelte'

  let message = $state('')
  const editable = $derived(can('admin'))
  $effect(() => {
    ai.load().catch((e) => (message = explain(e).message))
  })
</script>

<FormError {message} />
{#if ai.vision && ai.text}
  <AiBand role="vision" profile={ai.vision} {editable} />
  <AiBand role="text" profile={ai.text} {editable} />
  <p class="m-0 px-4 py-3 text-[12px] text-muted md:px-6 md:max-w-2xl">Photos and text are sent to the endpoint you set here, and nowhere else. The AI is optional: everything else works without it.</p>
{:else if !message}
  <p class="m-0 px-4 py-4 text-[14px] text-muted md:px-6">Loading</p>
{/if}

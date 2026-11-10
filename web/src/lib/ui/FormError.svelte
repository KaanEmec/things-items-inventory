<script lang="ts">
  let { message }: { message?: string } = $props()
</script>

{#if message}
  <p role="alert" class="rounded-control border border-repair-fg/25 bg-repair-bg px-3 py-2 text-[13px] text-repair-fg">{message}</p>
{/if}

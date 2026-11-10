<script lang="ts">
  // Placeholder rows while a list loads. Hidden for the first 300 ms so quick loads show nothing.
  let { rows = 5, label = 'Loading' }: { rows?: number; label?: string } = $props()
</script>

<div class="skeleton" role="status">
  <span class="sr-only">{label}</span>
  <div aria-hidden="true">
    {#each Array.from({ length: rows }, (_, i) => i) as i (i)}
      <div class="flex items-center gap-3 border-b border-divider-row px-4 py-3 md:px-6">
        <span class="size-9 flex-none rounded-control bg-chrome-hover"></span>
        <span class="flex min-w-0 flex-1 flex-col gap-1.5">
          <span class="h-3 rounded-full bg-chrome-hover" style:width="{55 + ((i * 17) % 35)}%"></span>
          <span class="h-2.5 w-1/3 rounded-full bg-chrome-hover"></span>
        </span>
      </div>
    {/each}
  </div>
</div>

<style>
  .skeleton {
    opacity: 0;
    animation: skeleton-in 0s linear 300ms forwards;
  }
  .skeleton :global(.bg-chrome-hover) {
    animation: skeleton-pulse 1.4s ease-in-out infinite;
  }
  @keyframes skeleton-in {
    to {
      opacity: 1;
    }
  }
  @keyframes skeleton-pulse {
    50% {
      opacity: 0.55;
    }
  }
</style>

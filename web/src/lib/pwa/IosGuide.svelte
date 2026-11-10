<script lang="ts">
  import Icon from '../Icon.svelte'
</script>

<!-- iOS has no install prompt: the steps happen in Safari's Share sheet. -->
<ol class="m-0 list-none space-y-2.5 p-0 text-[14px] text-body">
  <li class="flex gap-2.5">
    <span class="flex size-6 flex-none items-center justify-center rounded-full bg-accent-soft font-mono text-[12px] font-semibold text-accent">1</span>
    <span>Tap the <strong class="font-semibold">Share</strong> button in Safari's toolbar (the square with an arrow).</span>
  </li>
  <li class="flex gap-2.5">
    <span class="flex size-6 flex-none items-center justify-center rounded-full bg-accent-soft font-mono text-[12px] font-semibold text-accent">2</span>
    <span>Scroll down and tap <strong class="font-semibold">Add to Home Screen</strong>.</span>
  </li>
  <li class="flex gap-2.5">
    <span class="flex size-6 flex-none items-center justify-center rounded-full bg-accent-soft font-mono text-[12px] font-semibold text-accent">3</span>
    <span>Tap <strong class="font-semibold">Add</strong>, then open the app from the Home Screen.</span>
  </li>
</ol>

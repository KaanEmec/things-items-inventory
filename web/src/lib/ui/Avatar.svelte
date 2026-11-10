<script lang="ts" module>
  const colors = ['var(--color-gadget-fg)', 'var(--color-clothing-fg)', 'var(--color-accessory-fg)', 'var(--color-part-fg)']
</script>

<script lang="ts">
  let { name, size = 22 }: { name: string; size?: number } = $props()
  const initials = $derived(
    name.trim().split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join(''),
  )
  const color = $derived(colors[[...name].reduce((h, c) => h + c.charCodeAt(0), 0) % colors.length])
</script>

<span
  class="inline-flex flex-none items-center justify-center rounded-full font-bold text-white"
  style:width="{size}px"
  style:height="{size}px"
  style:font-size="{Math.round(size * 0.45)}px"
  style:background-color={color}
  role="img"
  aria-label={name}
>
  {initials}
</span>

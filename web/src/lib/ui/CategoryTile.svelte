<script lang="ts" module>
  import type { IconName } from '../icons'

  const categories = {
    gadget: { icon: 'device', cls: 'bg-gadget-bg text-gadget-fg' },
    clothing: { icon: 'shirt', cls: 'bg-clothing-bg text-clothing-fg' },
    accessory: { icon: 'watch', cls: 'bg-accessory-bg text-accessory-fg' },
    part: { icon: 'chip', cls: 'bg-part-bg text-part-fg' },
    other: { icon: 'package', cls: 'bg-stored-bg text-stored-fg' },
  } as const satisfies Record<string, { icon: IconName; cls: string }>

  export type Category = keyof typeof categories
  export const categoryKeys = Object.keys(categories) as Category[]
</script>

<script lang="ts">
  import Icon from '../Icon.svelte'

  /** `icon` overrides the category glyph (e.g. a drone for a gadget). */
  let { category, icon, size = 34 }: { category: Category | (string & {}); icon?: IconName; size?: number } = $props()
  const c = $derived(categories[category as Category] ?? categories.other)
</script>

<span
  class="inline-flex flex-none items-center justify-center rounded-[9px] {c.cls}"
  style:width="{size}px"
  style:height="{size}px"
  title={category}
>
  <Icon name={icon ?? c.icon} size={size >= 38 ? 18 : 16} />
</span>

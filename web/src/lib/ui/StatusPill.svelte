<script lang="ts" module>
  import type { IconName } from '../icons'

  const statuses = {
    'in-use': { label: 'In use', icon: 'pulse', cls: 'bg-use-bg text-use-fg' },
    stored: { label: 'Stored', icon: 'archive', cls: 'bg-stored-bg text-stored-fg' },
    sealed: { label: 'Sealed', icon: 'package', cls: 'bg-sealed-bg text-sealed-fg' },
    'needs-repair': { label: 'Needs repair', icon: 'wrench', cls: 'bg-repair-bg text-repair-fg' },
    'sell-donate': { label: 'To sell / donate', icon: 'gift', cls: 'bg-sell-bg text-sell-fg' },
    'ai-draft': { label: 'Draft', icon: 'sparkle', cls: 'bg-ai-bg text-ai-fg' },
    // Item statuses as the server names them.
    in_use: { label: 'In use', icon: 'pulse', cls: 'bg-use-bg text-use-fg' },
    packed: { label: 'Packed', icon: 'package', cls: 'bg-sealed-bg text-sealed-fg' },
    to_sell: { label: 'To sell / donate', icon: 'gift', cls: 'bg-sell-bg text-sell-fg' },
    lent: { label: 'Lent out', icon: 'user', cls: 'bg-stored-bg text-stored-fg' },
    gone: { label: 'Gone', icon: 'x', cls: 'bg-stored-bg text-stored-fg' },
  } as const satisfies Record<string, { label: string; icon: IconName; cls: string }>

  export type Status = keyof typeof statuses
  export const statusKeys = Object.keys(statuses) as Status[]
  /** Label, icon and colours for a status; unknown values look like "stored". */
  export const statusMeta = (s: string) => statuses[s as Status] ?? statuses.stored
</script>

<script lang="ts">
  import Icon from '../Icon.svelte'

  let { status, label }: { status: Status | (string & {}); label?: string } = $props()
  const s = $derived(statusMeta(status))
</script>

<span class="inline-flex h-6 items-center gap-[5px] rounded-full pr-[9px] pl-[7px] text-[12px] font-semibold whitespace-nowrap {s.cls}">
  <Icon name={s.icon} size={13} />{label ?? s.label}
</span>

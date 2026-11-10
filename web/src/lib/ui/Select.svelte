<script lang="ts">
  import type { HTMLSelectAttributes } from 'svelte/elements'
  import Icon from '../Icon.svelte'

  interface Props extends Omit<HTMLSelectAttributes, 'class' | 'value'> {
    label: string
    options: { value: string; label: string }[]
    value?: string
  }
  let { label, options, value = $bindable(''), ...rest }: Props = $props()
  const id = $props.id()
</script>

<div class="flex flex-col gap-1.5">
  <label for={id} class="text-[13px] font-medium text-body">{label}</label>
  <div class="relative">
    <select
      {id}
      bind:value
      class="h-11 w-full appearance-none rounded-control border border-border-input bg-surface pr-9 pl-3 text-[16px] text-ink md:h-9 md:text-[14px]"
      {...rest}
    >
      {#each options as o (o.value)}
        <option value={o.value}>{o.label}</option>
      {/each}
    </select>
    <Icon name="chevron-down" class="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-muted" />
  </div>
</div>

<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements'
  import Icon from '../Icon.svelte'

  interface Props extends Omit<HTMLInputAttributes, 'class'> {
    label: string
    hint?: string
    error?: string
    /** AI-filled value: violet background, sparkle and "AI" badge. */
    ai?: boolean
    value?: string
  }
  let { label, hint, error, ai = false, value = $bindable(''), ...rest }: Props = $props()
  const id = $props.id()
</script>

<div class="flex flex-col gap-1.5">
  <label for={id} class="flex items-center gap-1.5 text-[13px] font-medium text-body">
    {label}
    {#if ai}
      <span class="inline-flex h-[18px] items-center gap-[3px] rounded-full bg-ai-bg px-1.5 text-[10px] font-bold text-ai-fg">
        <Icon name="sparkle" size={9} />AI
      </span>
    {/if}
  </label>
  <div class="relative">
    {#if ai}<Icon name="sparkle" class="absolute top-1/2 left-3 -translate-y-1/2 text-ai-fg" />{/if}
    <input
      {id}
      bind:value
      aria-invalid={error ? true : undefined}
      aria-describedby={error || hint ? `${id}-note` : undefined}
      class="h-11 w-full rounded-control border px-3 text-[16px] text-ink placeholder:text-faint md:h-9 md:text-[14px]
        {ai ? 'border-ai-field-border bg-ai-field-bg pl-9' : 'border-border-input bg-surface'}
        {error ? 'border-danger' : ''}"
      {...rest}
    />
  </div>
  {#if error || hint}
    <p id="{id}-note" class="text-[12px] {error ? 'text-danger' : 'text-muted'}">{error ?? hint}</p>
  {/if}
</div>

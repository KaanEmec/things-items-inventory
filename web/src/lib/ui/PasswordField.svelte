<script lang="ts">
  import Icon from '../Icon.svelte'

  interface Props {
    label: string
    value?: string
    error?: string
    hint?: string
    autocomplete: 'current-password' | 'new-password'
    name?: string
  }
  let { label, value = $bindable(''), error, hint, autocomplete, name }: Props = $props()
  const id = $props.id()
  let shown = $state(false)
</script>

<div class="flex flex-col gap-1.5">
  <label for={id} class="text-[13px] font-medium text-body">{label}</label>
  <div class="relative">
    <input
      {id}
      {name}
      {autocomplete}
      type={shown ? 'text' : 'password'}
      bind:value
      aria-invalid={error ? true : undefined}
      aria-describedby={error || hint ? `${id}-note` : undefined}
      class="h-11 w-full rounded-control border bg-surface pr-11 pl-3 text-[16px] text-ink md:h-9 md:pr-10 md:text-[14px] {error ? 'border-danger' : 'border-border-input'}"
    />
    <button
      type="button"
      onclick={() => (shown = !shown)}
      aria-label={shown ? 'Hide password' : 'Show password'}
      aria-pressed={shown}
      class="absolute top-0 right-0 flex size-11 cursor-pointer items-center justify-center rounded-control text-muted hover:text-ink md:size-9"
    >
      <Icon name={shown ? 'eye-off' : 'eye'} size={18} />
    </button>
  </div>
  {#if error || hint}
    <p id="{id}-note" class="text-[12px] {error ? 'text-danger' : 'text-muted'}">{error ?? hint}</p>
  {/if}
</div>

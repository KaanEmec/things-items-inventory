<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { HTMLButtonAttributes } from 'svelte/elements'
  import Icon from '../Icon.svelte'
  import type { IconName } from '../icons'

  interface Props extends Omit<HTMLButtonAttributes, 'class'> {
    variant?: 'primary' | 'secondary' | 'ghost'
    icon?: IconName
    /** Renders a link instead of a button. */
    href?: string
    class?: string
    children?: Snippet
  }
  let { variant = 'secondary', icon, href, class: cls = '', children, ...rest }: Props = $props()

  const styles = {
    primary: 'border-transparent bg-accent text-white shadow-primary hover:bg-accent-hover active:bg-accent-pressed',
    secondary: 'border-border-input bg-surface text-ink shadow-button hover:bg-surface-subtle [&>svg]:text-muted',
    ghost: 'border-transparent bg-transparent text-body hover:bg-chrome-hover',
  }
</script>

<svelte:element
  this={href ? 'a' : 'button'}
  {href}
  type={href ? undefined : 'button'}
  class="inline-flex h-11 cursor-pointer items-center justify-center gap-[7px] rounded-control border px-4 text-[14px] font-semibold whitespace-nowrap select-none disabled:cursor-not-allowed disabled:opacity-50 md:h-9 md:px-[13px] md:text-[13px] {styles[variant]} {cls}"
  {...rest}
>
  {#if icon}<Icon name={icon} />{/if}
  {@render children?.()}
</svelte:element>

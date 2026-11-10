<script lang="ts">
  import { icons, type IconName } from './icons'

  interface Props {
    name: IconName
    size?: number
    class?: string
  }
  let { name, size = 16, class: cls = '' }: Props = $props()
</script>

<!-- Markup comes from the static icon table, never from user input. -->
<svg
  class="flex-none {cls}"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={size <= 13 ? 2 : 1.75}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {@html icons[name]}
</svg>

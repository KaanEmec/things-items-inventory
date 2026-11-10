<script lang="ts">
  // Two hidden file inputs: the system camera (works in an installed iOS PWA) and the photo library.
  interface Props {
    onfiles: (files: File[], tag: string) => void
  }
  let { onfiles }: Props = $props()

  let camera: HTMLInputElement
  let library: HTMLInputElement
  let tag = ''

  export function openCamera(t = '') {
    tag = t
    camera.click()
  }
  export function openLibrary(t = '') {
    tag = t
    library.click()
  }
  function picked(e: Event) {
    const input = e.currentTarget as HTMLInputElement
    const files = [...(input.files ?? [])]
    input.value = '' // the same photo can be chosen again
    if (files.length) onfiles(files, tag)
  }
</script>

<input bind:this={camera} type="file" accept="image/*" capture="environment" class="hidden" onchange={picked} tabindex="-1" aria-hidden="true" />
<input bind:this={library} type="file" accept="image/*" multiple class="hidden" onchange={picked} tabindex="-1" aria-hidden="true" />

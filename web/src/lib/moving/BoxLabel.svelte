<script lang="ts">
  import Icon from '../Icon.svelte'
  import type { Handling, GoesTo } from './boxes'
  import { HANDLING_ICON, HANDLING_LABEL, patternFor, roomTint } from './print'
  import { qrSvg } from './qr'

  interface Props {
    number: number | null
    /** Destination room; `null` shows a neutral "NO ROOM YET" band. */
    room: Pick<GoesTo, 'name' | 'color'> | null
    handling?: Handling[]
    household?: string
    /** `grid`: in the app, fills its container. `print`: 190 x 80 mm on paper. Same layout either way. */
    size?: 'grid' | 'print'
    /** Black and white: the band becomes a pattern and the room name carries the meaning. */
    mono?: boolean
    /** Link for a QR code; leave empty for none. */
    qr?: string
    class?: string
  }
  let { number, room, handling = [], household = '', size = 'grid', mono = false, qr = '', class: cls = '' }: Props = $props()

  const tint = $derived(roomTint(room?.color))
  const name = $derived((room?.name ?? 'No room yet').toUpperCase())
  const pattern = $derived(room ? patternFor(room.name) : 'none')
  const svg = $derived(qr ? qrSvg(qr) : '')
  // Longer numbers shrink so they still fit beside the QR code.
  const digits = $derived(String(number ?? '').length)
  const numSize = $derived(digits <= 2 ? 34 : digits === 3 ? 28 : digits === 4 ? 22 : 17)
  const aria = $derived(`Box ${number ?? ''}, ${room ? `goes to ${room.name}` : 'no room yet'}${handling.length ? ', ' + handling.map((h) => HANDLING_LABEL[h]).join(', ') : ''}`)
</script>

<div class="box-label {size} {cls}" class:mono role="img" aria-label={aria} style:--band={tint.bg} style:--band-ink={tint.fg} style:--num-size="{numSize}cqw">
  <div class="band" data-pattern={mono ? pattern : 'none'}>
    <span class="room"><span class="room-text">{name}</span></span>
    <span class="marks">
      {#each handling as h (h)}
        <Icon name={HANDLING_ICON[h]} size={20} class="mark" />
      {/each}
    </span>
  </div>
  <div class="body">
    <span class="num">{number ?? ''}</span>
    {#if svg}<span class="qr" aria-hidden="true">{@html svg}</span>{/if}
  </div>
  <div class="foot">{household}</div>
</div>

<style>
  .box-label {
    container-type: inline-size;
    box-sizing: border-box;
    width: 100%;
    aspect-ratio: 190 / 80;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: #fff;
    color: #1a1a19;
    border: 2px solid #1a1a19;
    border-radius: 8px;
    -webkit-print-color-adjust: exact;
    print-color-adjust: exact;
    break-inside: avoid;
    font-family: 'Inter Variable', system-ui, sans-serif;
  }
  .box-label.print {
    width: 100%;
    border-radius: 2.5mm;
    border-width: 2px;
  }
  .band {
    flex: none;
    height: 8.5cqw;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 4cqw;
    background: var(--band);
    color: var(--band-ink);
    border-bottom: 2px solid #1a1a19;
  }
  .room {
    font-weight: 800;
    font-size: 4.2cqw;
    letter-spacing: 0.09em;
    line-height: 1;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: clip;
  }
  .marks {
    display: flex;
    gap: 1.6cqw;
    align-items: center;
  }
  .marks :global(.mark) {
    width: 5.2cqw;
    height: 5.2cqw;
    stroke-width: 2.6;
  }
  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 4cqw;
    padding: 0 4cqw;
  }
  .num {
    font-weight: 800;
    font-size: var(--num-size, 34cqw);
    line-height: 0.8;
    letter-spacing: -0.05em;
    font-variant-numeric: tabular-nums;
  }
  .qr {
    flex: none;
    width: 22cqw;
    height: 22cqw;
    display: block;
  }
  .qr :global(svg) {
    width: 100%;
    height: 100%;
    display: block;
  }
  .foot {
    flex: none;
    padding: 0 4cqw 1.6cqw;
    font-size: 2.6cqw;
    line-height: 1;
    color: #5f5f5a;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* Black and white: pattern band, name on a white plate, everything black. */
  .mono .band {
    --band-ink: #1a1a19;
    background-color: #fff;
  }
  .mono .room-text {
    background: #fff;
    padding: 1cqw 2cqw;
    border: 1.5px solid #1a1a19;
  }
  .mono .foot {
    color: #1a1a19;
  }
  .band[data-pattern='stripes'] {
    background-image: repeating-linear-gradient(45deg, #1a1a19 0 1.2cqw, #fff 1.2cqw 3cqw);
  }
  .band[data-pattern='dots'] {
    background-image: radial-gradient(#1a1a19 22%, transparent 24%);
    background-size: 2.4cqw 2.4cqw;
  }
  .band[data-pattern='check'] {
    background-image: conic-gradient(#1a1a19 25%, #fff 0 50%, #1a1a19 0 75%, #fff 0);
    background-size: 3.2cqw 3.2cqw;
  }
  .band[data-pattern='cross'] {
    background-image:
      repeating-linear-gradient(45deg, #1a1a19 0 0.5cqw, transparent 0.5cqw 2.4cqw),
      repeating-linear-gradient(-45deg, #1a1a19 0 0.5cqw, transparent 0.5cqw 2.4cqw);
  }
  .band[data-pattern='hlines'] {
    background-image: repeating-linear-gradient(0deg, #1a1a19 0 0.9cqw, #fff 0.9cqw 2.2cqw);
  }
  /* Handling marks sit on a plate in mono so a pattern cannot swallow them. */
  .mono .marks {
    background: #fff;
    padding: 0.8cqw 1.6cqw;
    border: 1.5px solid #1a1a19;
  }
</style>

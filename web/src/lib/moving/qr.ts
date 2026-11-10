import qrcode from 'qrcode-generator'

/** SVG markup for a QR code of `text` (our own link, never user markup). */
export function qrSvg(text: string): string {
  const q = qrcode(0, 'M')
  q.addData(text)
  q.make()
  return q.createSvgTag({ cellSize: 4, margin: 0, scalable: true })
}

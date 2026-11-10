import type { IconName } from '../icons'

export const kindIcon: Record<string, IconName> = { floor: 'stairs', room: 'door', furniture: 'cabinet', spot: 'drawer', box: 'box' }

/** Room colour names the server knows, as a dot colour. */
export const roomColor: Record<string, string> = {
  red: '#d6453d',
  orange: '#e8822a',
  yellow: '#e0b422',
  green: '#3f9d5a',
  teal: '#2a9d9d',
  blue: '#2f5bea',
  purple: '#7c5ce0',
  pink: '#d9609a',
  brown: '#8a5a3c',
  grey: '#8a8a84',
}

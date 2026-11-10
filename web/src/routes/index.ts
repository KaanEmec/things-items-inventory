import type { Component } from 'svelte'
import type { IconName } from '../lib/icons'
import Capture from './Capture.svelte'
import Components from './dev/Components.svelte'
import Items from './Items.svelte'
import Moving from './Moving.svelte'
import Overview from './Overview.svelte'
import Places from './Places.svelte'
import QuickAdd from './QuickAdd.svelte'
import Settings from './Settings.svelte'

export interface Route {
  path: string
  title: string
  icon: IconName
  component: Component
  /** Also match sub-paths (`/settings/members`). */
  prefix?: boolean
  /** Section of the desktop side navigation; omit to hide from it. */
  side?: 'library' | 'tools' | 'foot'
  /** Label in the phone tab bar; omit to hide from it. */
  tab?: string
}

// To add a screen: create src/routes/X.svelte and add a row here.
export const routes: Route[] = [
  { path: '/', title: 'Overview', icon: 'dashboard', component: Overview, side: 'library', tab: 'Home' },
  { path: '/items', title: 'Items', icon: 'tag', component: Items, side: 'library', tab: 'Items' },
  { path: '/capture', title: 'Capture', icon: 'camera', component: Capture, tab: 'Capture' },
  { path: '/places', title: 'Places', icon: 'pin', component: Places, side: 'library', tab: 'Places' },
  { path: '/moving', title: 'Moving', icon: 'truck', component: Moving, side: 'tools', tab: 'Moving' },
  { path: '/quick-add', title: 'Quick add', icon: 'table', component: QuickAdd, side: 'tools' },
  { path: '/settings', title: 'Settings', icon: 'sliders', component: Settings, prefix: true, side: 'foot' },
  // Hidden: reachable by URL only.
  { path: '/dev/components', title: 'Components', icon: 'sliders', component: Components },
]

export const findRoute = (path: string) =>
  routes.find((r) => r.path === path) ?? routes.find((r) => r.prefix && path.startsWith(r.path + '/'))

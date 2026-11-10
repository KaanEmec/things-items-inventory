import type { Component } from 'svelte'
import { can, type Role } from '../../lib/auth.svelte'
import type { IconName } from '../../lib/icons'
import Account from './Account.svelte'
import AI from './AI.svelte'
import App from './App.svelte'
import Household from './Household.svelte'
import Members from './Members.svelte'
import Tokens from './Tokens.svelte'

export interface SettingsSection {
  /** Used in the address: /settings/<id>. */
  id: string
  title: string
  icon: IconName
  /** One line under the title on phone. */
  summary: string
  component: Component
  /** Only people with at least this role see the section. */
  minRole?: Role
}

// To add a Settings section: create settings/X.svelte and add one entry here.
export const sections: SettingsSection[] = [
  { id: 'household', title: 'Household', icon: 'home', summary: 'Name, language, currency, warranty', component: Household },
  { id: 'members', title: 'Members', icon: 'user', summary: 'Who is in the household, invites', component: Members },
  { id: 'ai', title: 'AI', icon: 'sparkle', summary: 'Photo understanding and text endpoints', component: AI },
  { id: 'tokens', title: 'Access tokens', icon: 'link', summary: 'For agents and scripts', component: Tokens, minRole: 'member' },
  { id: 'account', title: 'Account', icon: 'shield', summary: 'Your name, email, password, sessions', component: Account },
  { id: 'app', title: 'App', icon: 'device', summary: 'Install on this device, updates', component: App },
]

export const visibleSections = () => sections.filter((s) => !s.minRole || can(s.minRole))

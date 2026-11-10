import { mount } from 'svelte'
import './app.css'
import './lib/pwa/pwa.css'
import App from './App.svelte'
import PwaBanners from './lib/pwa/PwaBanners.svelte'
import { pwa } from './lib/pwa/pwa.svelte'

mount(App, { target: document.getElementById('app')! })

// Update, offline and iOS-guide notices live in their own root, beside the app.
pwa.start()
mount(PwaBanners, { target: document.body })

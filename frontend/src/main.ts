import { createPinia } from 'pinia'
import { createApp } from 'vue'
import App from './App.vue'
import { onSessionEnd } from './api/client'
import { i18n } from './i18n'
import { router } from './router'
import { useSessionStore } from './stores/session'
import './style.css'
import { applyDateFormat, applyTimeFormat, readStoredDateFormat, readStoredTimeFormat } from './utils/display'
import { applyTheme, readStoredTheme } from './utils/theme'
import { applyUnits, readStoredUnits } from './utils/units'

// The last theme used on this browser is shown before anything loads, so the
// page does not flash light and then turn dark.
applyTheme(readStoredTheme())
applyUnits(readStoredUnits())
applyDateFormat(readStoredDateFormat())
applyTimeFormat(readStoredTimeFormat())

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(router)
app.use(i18n)

// A session the server no longer accepts sends the person to sign in again,
// back to the page they were on afterwards.
onSessionEnd(() => {
  useSessionStore(pinia).forget()
  const current = router.currentRoute.value
  if (!current.meta.public) {
    void router.push({ name: 'login', query: { redirect: current.fullPath } })
  }
})

app.mount('#app')

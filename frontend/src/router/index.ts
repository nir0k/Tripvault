import { createRouter, createWebHistory } from 'vue-router'
import type { DocumentKind } from '@/api/types'
import { useSessionStore } from '@/stores/session'

declare module 'vue-router' {
  interface RouteMeta {
    /** Reachable without signing in. */
    public?: boolean
    /** Rendered without the application menu. */
    bare?: boolean
    /** Administrators only. */
    admin?: boolean
    /** Uses the full width of the window instead of the reading width. */
    wide?: boolean
    /** The section a page belongs to: the plans or the reports. */
    kind?: DocumentKind
  }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true, bare: true },
    },
    {
      // A read-only link: https://<host>/s#token=<token>. The fragment never
      // reaches the server, so the address itself carries no credential into a log.
      path: '/s',
      name: 'shared',
      component: () => import('@/views/SharedView.vue'),
      meta: { public: true, bare: true },
    },
    {
      path: '/change-password',
      name: 'change-password',
      component: () => import('@/views/ChangePasswordView.vue'),
      meta: { bare: true },
    },
    {
      // The start page: the nearest plans and the reports changed last, each
      // leading on to its full list.
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
    {
      path: '/trips',
      name: 'trips',
      component: () => import('@/views/TripsView.vue'),
      props: { kind: 'plan' },
      meta: { kind: 'plan' },
    },
    {
      path: '/reports',
      name: 'reports',
      component: () => import('@/views/TripsView.vue'),
      props: { kind: 'report' },
      meta: { kind: 'report' },
    },
    {
      path: '/trips/:tripId',
      component: () => import('@/views/trip/TripLayout.vue'),
      // The width belongs to the trip, not to one of its tabs: the title and
      // the tab bar live in the same column, so a tab of a different width
      // would shift them as well as its own content.
      meta: { wide: true, kind: 'plan' },
      children: [
        { path: '', redirect: (to) => ({ name: 'trip-plan', params: to.params }) },
        { path: 'plan', name: 'trip-plan', component: () => import('@/views/trip/TripPlanView.vue') },
        { path: 'media', name: 'trip-media', component: () => import('@/views/trip/TripMediaView.vue') },
        { path: 'budget', name: 'trip-budget', component: () => import('@/views/trip/TripBudgetView.vue') },
        { path: 'settings', name: 'trip-settings', component: () => import('@/views/trip/TripSettingsView.vue') },
      ],
    },
    {
      // A report is a trip of its own, under its own section; its pages are
      // the same components as a plan's, reading the trip they are given.
      path: '/reports/:tripId',
      component: () => import('@/views/trip/TripLayout.vue'),
      meta: { wide: true, kind: 'report' },
      children: [
        { path: '', redirect: (to) => ({ name: 'report-report', params: to.params }) },
        { path: 'report', name: 'report-report', component: () => import('@/views/trip/TripDocumentView.vue') },
        { path: 'media', name: 'report-media', component: () => import('@/views/trip/TripMediaView.vue') },
        { path: 'budget', name: 'report-budget', component: () => import('@/views/trip/TripBudgetView.vue') },
        { path: 'settings', name: 'report-settings', component: () => import('@/views/trip/TripSettingsView.vue') },
      ],
    },
    { path: '/profile', name: 'profile', component: () => import('@/views/ProfileView.vue') },
    {
      path: '/admin/users',
      name: 'admin-users',
      component: () => import('@/views/admin/AdminUsersView.vue'),
      meta: { admin: true },
    },
    {
      path: '/admin/backups',
      name: 'admin-backups',
      component: () => import('@/views/admin/AdminBackupsView.vue'),
      meta: { admin: true },
    },
    {
      path: '/admin/status',
      name: 'admin-status',
      component: () => import('@/views/admin/AdminStatusView.vue'),
      meta: { admin: true },
    },
    { path: '/:pathMatch(.*)*', redirect: { name: 'home' } },
  ],
})

// The guard is cosmetic: it keeps people on pages that will work for them.
// Every rule it follows is enforced by the API regardless.
router.beforeEach(async (to) => {
  const session = useSessionStore()
  await session.restore()

  // Signing in again makes no sense; every other public page does, so an owner
  // signed in here can still open a link they handed out and see what it shows.
  if (to.name === 'login') {
    return session.signedIn ? { name: 'home' } : true
  }
  if (to.meta.public) {
    return true
  }
  if (!session.signedIn) {
    return { name: 'login', query: to.fullPath === '/' ? {} : { redirect: to.fullPath } }
  }
  if (session.mustChangePassword && to.name !== 'change-password') {
    return { name: 'change-password', query: { redirect: to.fullPath } }
  }
  if (!session.mustChangePassword && to.name === 'change-password') {
    return { name: 'home' }
  }
  if (to.meta.admin && !session.isAdmin) {
    return { name: 'home' }
  }
  return true
})

/** safeRedirect accepts only a path inside this application. */
export function safeRedirect(value: unknown): string {
  return typeof value === 'string' && value.startsWith('/') && !value.startsWith('//') ? value : '/'
}

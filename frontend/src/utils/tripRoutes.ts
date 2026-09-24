import type { RouteLocationRaw } from 'vue-router'
import type { DocumentKind } from '@/api/types'

// A plan and a report are separate trips, each under its own section of the
// application: plans under /trips, reports under /reports. They share the pages
// inside - photos, budget, settings - but not the route names, so the menu can
// tell which section is open and a link can never land a report under the plans.

/** TripSection is a page inside a trip: its document, or one of the pages beside it. */
export type TripSection = 'document' | 'media' | 'budget' | 'settings'

/** TRIP_SECTIONS lists the pages of a trip in the order its tabs show them. */
export const TRIP_SECTIONS: readonly TripSection[] = ['document', 'media', 'budget', 'settings']

// The route names of each kind's pages, as the router declares them.
const ROUTE_NAMES: Record<DocumentKind, Record<TripSection, string>> = {
  plan: { document: 'trip-plan', media: 'trip-media', budget: 'trip-budget', settings: 'trip-settings' },
  report: { document: 'report-report', media: 'report-media', budget: 'report-budget', settings: 'report-settings' },
}

/** tripRouteName names the route of one page of a plan or a report. */
export function tripRouteName(kind: DocumentKind, section: TripSection = 'document'): string {
  return ROUTE_NAMES[kind][section]
}

/**
 * tripRoute builds the address of one page of a trip.
 *
 * Arguments:
 *   - trip: the trip, or just its identifier and kind.
 *   - section: the page; the document by default.
 *
 * Returns:
 *   - a location the router and RouterLink accept.
 */
export function tripRoute(trip: { id: string; kind: DocumentKind }, section: TripSection = 'document'): RouteLocationRaw {
  return { name: tripRouteName(trip.kind, section), params: { tripId: trip.id } }
}

/** sectionOfRoute finds which page of a trip a route name is, or null for any other page. */
export function sectionOfRoute(name: unknown): TripSection | null {
  for (const names of Object.values(ROUTE_NAMES)) {
    for (const section of TRIP_SECTIONS) {
      if (names[section] === name) {
        return section
      }
    }
  }
  return null
}

/** listRouteName names the list a kind of trip is found in. */
export function listRouteName(kind: DocumentKind): string {
  return kind === 'report' ? 'reports' : 'trips'
}

import { inject, provide, type InjectionKey } from 'vue'
import type { PlanItem, Stop } from '@/api/types'

// The stops along the line of an activity are changed from two places: the
// activity's card, which lists them, and the map, where a click on a line puts
// a stop there. Both open the same forms, which the page holds once
// (StopEditor) and offers to everything below it here, so a card deep in a list
// need not pass a request up through every list it sits in.

/** StopEditing opens the forms a stop is changed through. */
export interface StopEditing {
  /** add opens the form of a new stop of an activity, at a point when one was picked. */
  add: (item: PlanItem, point?: { lat: number; lng: number }) => void
  /** edit opens the form of a stop. */
  edit: (item: PlanItem, stop: Stop) => void
  /** editCost opens the form of a stop's cost. */
  editCost: (item: PlanItem, stop: Stop) => void
  /** remove deletes a stop after asking. */
  remove: (item: PlanItem, stop: Stop) => void
}

/** stopEditingKey carries the page's StopEditing. */
const stopEditingKey: InjectionKey<StopEditing> = Symbol('stopEditing')

/**
 * provideStopEditing - offers the page's stop forms to the cards and the map
 * below it.
 *
 * Arguments:
 *   - editing: what opens the forms.
 */
export function provideStopEditing(editing: StopEditing): void {
  provide(stopEditingKey, editing)
}

/**
 * useStopEditing - returns what opens the page's stop forms.
 *
 * Returns:
 *   - the provided forms, or null on a page that changes no stops.
 */
export function useStopEditing(): StopEditing | null {
  return inject(stopEditingKey, null)
}

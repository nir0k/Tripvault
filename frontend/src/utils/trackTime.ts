import type { Stop, Track } from '@/api/types'

// How long a plan's line takes to walk, and the scale its speed is chosen on.
//
// The server measures how many metres of the line run at each slope (grades);
// the time is worked out here, so the speed can be moved and the time follows
// without asking anybody. Each stretch is walked at Tobler's pace for its
// slope, scaled so that the flat is walked at the speed chosen: a gentle
// descent a little faster, a climb or a steep descent slower.

/** MIN_SPEED and MAX_SPEED bound the speed on the flat, in km/h, as the server does. */
export const MIN_SPEED = 1
export const MAX_SPEED = 12

/** DEFAULT_SPEED is the speed a plan starts at: a steady walk. */
export const DEFAULT_SPEED = 4.7

/**
 * SPEED_STOPS are the marks of the scale, from a slow stroll to a brisk walk,
 * each an equal step of the slider however far apart the speeds are, so the
 * speeds people walk at take most of it and running fills only the last step.
 */
export const SPEED_STOPS: readonly number[] = [2, 2.8, 3.5, 4.7, 5.5, 6.5]

// SCALE is every point the slider is drawn through, its two ends included.
const SCALE: readonly number[] = [MIN_SPEED, ...SPEED_STOPS, MAX_SPEED]

/** SLIDER_STEPS is how many steps the slider has from end to end. */
export const SLIDER_STEPS = (SCALE.length - 1) * 100

// SNAP is how close to a mark, in steps of the slider, the thumb is drawn onto it.
const SNAP = 12

// GRADE_0 is the index of the flat in a track's grades, from -50 % at index 0.
const GRADE_0 = 50

/**
 * toblerFactor is how much faster than on the flat a slope is walked, by
 * Tobler's hiking function: fastest on a descent of 5 %, slower either way.
 */
export function toblerFactor(slope: number): number {
  return Math.exp(-3.5 * (Math.abs(slope + 0.05) - 0.05))
}

/**
 * walkingSeconds works out how long a line takes at a speed on the flat. The
 * metres of the line its grades do not account for - a line without heights,
 * or the ends before the first height - are walked as flat.
 *
 * Returns:
 *   - the time in seconds.
 */
export function walkingSeconds(track: Pick<Track, 'distance_m' | 'grades'>, speedKmh: number): number {
  const flat = speedKmh / 3.6
  let seconds = 0
  let counted = 0
  track.grades?.forEach((metres, index) => {
    seconds += metres / (flat * toblerFactor((index - GRADE_0) / 100))
    counted += metres
  })
  return seconds + Math.max(track.distance_m - counted, 0) / flat
}

/**
 * stopSeconds works out how long the way from the start of a line to a stop
 * along it takes at a speed on the flat, from the slopes of the way there.
 *
 * Returns:
 *   - the time in seconds.
 */
export function stopSeconds(stop: Pick<Stop, 'distance_m' | 'grades_to'>, speedKmh: number): number {
  return walkingSeconds({ distance_m: stop.distance_m, grades: stop.grades_to }, speedKmh)
}

/** clampSpeed keeps a speed within the scale, to a tenth. */
export function clampSpeed(speed: number): number {
  return Math.round(Math.min(Math.max(speed, MIN_SPEED), MAX_SPEED) * 10) / 10
}

/**
 * speedAt reads the speed at a position of the slider: along the step it is
 * in, drawn onto a mark close enough to it.
 *
 * Arguments:
 *   - position: the slider's value, 0 to SLIDER_STEPS.
 *
 * Returns:
 *   - the speed in km/h, to a tenth.
 */
export function speedAt(position: number): number {
  const clamped = Math.min(Math.max(position, 0), SLIDER_STEPS)
  const mark = Math.round(clamped / 100)
  if (mark > 0 && mark < SCALE.length - 1 && Math.abs(clamped - mark * 100) <= SNAP) {
    return SCALE[mark] ?? DEFAULT_SPEED
  }
  const step = Math.min(Math.floor(clamped / 100), SCALE.length - 2)
  const from = SCALE[step] ?? MIN_SPEED
  const to = SCALE[step + 1] ?? MAX_SPEED
  return clampSpeed(from + (to - from) * ((clamped - step * 100) / 100))
}

/**
 * positionOf finds where a speed sits on the slider.
 *
 * Returns:
 *   - the slider's value, 0 to SLIDER_STEPS.
 */
export function positionOf(speed: number): number {
  const clamped = Math.min(Math.max(speed, MIN_SPEED), MAX_SPEED)
  for (let step = 0; step < SCALE.length - 1; step++) {
    const from = SCALE[step] ?? MIN_SPEED
    const to = SCALE[step + 1] ?? MAX_SPEED
    if (clamped <= to) {
      return Math.round(step * 100 + ((clamped - from) / (to - from)) * 100)
    }
  }
  return SLIDER_STEPS
}

/** stopPosition is where a mark of SPEED_STOPS sits on the slider. */
export function stopPosition(index: number): number {
  return (index + 1) * 100
}

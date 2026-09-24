import { onBeforeUnmount, watch, type Ref } from 'vue'
import * as documentsApi from '@/api/documents'
import type { TripDocument } from '@/api/types'

// CALCULATION_DELAY is how long edits must pause before waiting legs are
// calculated, so a burst of moves costs one round of provider requests.
const CALCULATION_DELAY = 800

/** LegCalculationOptions is what the calculation needs from the page using it. */
export interface LegCalculationOptions {
  /** The document on screen, replaced by the calculated one when it answers. */
  document: Ref<TripDocument | null>
  /** Whether the reader may change the document, which calculating does. */
  enabled: () => boolean
  /** Whether the page is saving something; the calculation waits for it, and a
   * retry of the estimates raises it while it runs. */
  busy: Ref<boolean>
  /** Shows why a calculation failed. */
  onError: (err: unknown) => void
}

/** LegCalculation is what a page can ask of the calculation itself. */
export interface LegCalculation {
  /** Asks the provider again about the legs that came back as estimates. */
  retryEstimates: () => Promise<void>
}

/**
 * useLegCalculation calculates the legs of a document that are waiting for a
 * distance and a time, once edits pause. It repeats while the server keeps
 * calculating a batch at a time, and stops when a round makes no progress so a
 * failing provider cannot cause a loop. What comes back is stored with the
 * legs, so a leg is asked about once and read afterwards: only a new leg, or
 * one whose mode or points changed, waits for a calculation. The plan and the
 * report share it.
 *
 * Arguments:
 *   - options: the document and the page's state.
 *
 * Returns:
 *   - the retry of the legs that came back as estimates.
 */
export function useLegCalculation(options: LegCalculationOptions): LegCalculation {
  let timer: ReturnType<typeof setTimeout> | undefined
  let calculating = false

  // schedule waits for edits to pause and then asks for one batch.
  function schedule(): void {
    clearTimeout(timer)
    if (!options.enabled() || !options.document.value || options.document.value.pending_legs === 0) {
      return
    }
    timer = setTimeout(async () => {
      const current = options.document.value
      if (!current || calculating || options.busy.value) {
        schedule()
        return
      }
      calculating = true
      const before = current.pending_legs
      try {
        const next = await documentsApi.calculateLegs(current.id)
        // An edit made meanwhile has its own answer; this one would be stale.
        if (options.document.value === current) {
          options.document.value = next
        }
        if (next.pending_legs > 0 && next.pending_legs < before) {
          schedule()
        }
      } catch (err) {
        options.onError(err)
      } finally {
        calculating = false
      }
    }, CALCULATION_DELAY)
  }

  /**
   * retryEstimates asks the provider again about the legs that came back as
   * straight lines, such as the ones a rate limit left behind. It repeats while
   * a round makes progress: the server takes a bounded number at a time, and a
   * long trip would otherwise need pressing over and over.
   */
  async function retryEstimates(): Promise<void> {
    const current = options.document.value
    if (!current || options.busy.value) {
      return
    }
    options.busy.value = true
    try {
      let left = current.estimated_legs
      while (left > 0) {
        const next = await documentsApi.retryEstimatedLegs(current.id)
        if (options.document.value === current || options.document.value?.id === next.id) {
          options.document.value = next
        }
        if (next.estimated_legs >= left) {
          // Nothing moved: the provider is answering the same way it did
          // before, and asking again would only spend its requests.
          break
        }
        left = next.estimated_legs
      }
    } catch (err) {
      options.onError(err)
    } finally {
      options.busy.value = false
    }
  }

  watch(() => [options.document.value?.pending_legs, options.enabled()], () => schedule())
  onBeforeUnmount(() => clearTimeout(timer))
  return { retryEstimates }
}

import { inject, provide, type InjectionKey } from 'vue'
import type { TripDocument } from '@/api/types'

// A change a card makes by itself - attaching a file to a place, removing one -
// answers with the whole document, like every other change of a plan or a
// report. The page that holds the document provides how it takes that answer,
// so the card need not pass it up through every list it sits in.

/** DocumentChange takes the document a change answered with. */
export type DocumentChange = (document: TripDocument) => void

/** documentChangeKey carries the page's DocumentChange. */
const documentChangeKey: InjectionKey<DocumentChange> = Symbol('documentChange')

/**
 * provideDocumentChange - tells the cards below this page how to hand back the
 * document a change of theirs answered with.
 *
 * Arguments:
 *   - take: stores the document as the page's own.
 */
export function provideDocumentChange(take: DocumentChange): void {
  provide(documentChangeKey, take)
}

/**
 * useDocumentChange - returns how the page takes a changed document.
 *
 * Returns:
 *   - the provided function, or null on a page that changes nothing.
 */
export function useDocumentChange(): DocumentChange | null {
  return inject(documentChangeKey, null)
}

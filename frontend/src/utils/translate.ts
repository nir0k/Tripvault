import type {
  DocumentTranslations, PlanItem, TranslationTarget, TripDocument, TripTranslations,
} from '@/api/types'

// A report may be written in several languages. Its own fields hold the
// original, in the first of its languages; every other language keeps, field
// by field, only what somebody translated. A reader is shown their own
// language when the report has it, and every field nobody translated falls
// back to the original, so a report half translated still reads as a whole.
//
// The server resolves the same way for the PDF (domain.Translated); the two
// are kept in step by hand.

/**
 * readingLanguage picks the language a report is shown in.
 *
 * Arguments:
 *   - languages: the report's languages, the original first.
 *   - requested: the language the reader asks for.
 *
 * Returns:
 *   - the requested language when the report has it, otherwise the original;
 *     '' for a trip without languages, which is a plan.
 */
export function readingLanguage(languages: readonly string[], requested: string): string {
  if (languages.includes(requested)) {
    return requested
  }
  return languages[0] ?? ''
}

/** translateTrip returns a trip's title and summary in a language, the original standing in. */
export function translateTrip<T extends { title: string; summary: string; translations?: TripTranslations }>(
  trip: T,
  lang: string,
): T {
  const fields = trip.translations?.[lang]
  if (!fields) {
    return trip
  }
  return { ...trip, title: fields.title || trip.title, summary: fields.summary || trip.summary }
}

/**
 * translateDocument returns a copy of a document with its words in a language.
 *
 * A field nobody translated keeps its original, and so does every field of
 * the original language itself. A stay mark shows its stay's name, so it
 * follows the translation of the stay.
 */
export function translateDocument(document: TripDocument, lang: string): TripDocument {
  const texts = document.translations?.[lang]
  if (!texts) {
    return document
  }
  const pick = (id: string, field: string, original: string): string => texts[id]?.[field] || original
  const item = (each: PlanItem): PlanItem => {
    if (each.kind === 'stay_anchor') {
      return each.stay_id ? { ...each, name: pick(each.stay_id, 'name', each.name) } : each
    }
    return {
      ...each,
      name: pick(each.id, 'name', each.name),
      description_md: pick(each.id, 'description_md', each.description_md),
      story_md: pick(each.id, 'story_md', each.story_md),
    }
  }
  return {
    ...document,
    intro_md: pick(document.id, 'intro_md', document.intro_md),
    summary_md: pick(document.id, 'summary_md', document.summary_md),
    days: document.days.map((day) => ({
      ...day,
      title: pick(day.id, 'title', day.title),
      notes_md: pick(day.id, 'notes_md', day.notes_md),
      items: day.items.map(item),
      legs: day.legs.map((leg) => ({ ...leg, note: pick(leg.id, 'note', leg.note) })),
    })),
    unassigned: document.unassigned.map(item),
    stays: document.stays.map((stay) => ({
      ...stay,
      name: pick(stay.id, 'name', stay.name),
      notes_md: pick(stay.id, 'notes_md', stay.notes_md),
    })),
  }
}

/** TranslatableText is one field of a report a translator works through. */
export interface TranslatableText {
  target: TranslationTarget
  id: string
  field: string
  original: string
}

/**
 * translatableTexts lists the fields of a report that can be translated from
 * this page and hold something to translate: the words around the days, the
 * days, the places and the journeys between them. The trip's own title and
 * summary are translated in its settings.
 */
export function translatableTexts(document: TripDocument): TranslatableText[] {
  const texts: TranslatableText[] = []
  const add = (target: TranslationTarget, id: string, field: string, original: string): void => {
    if (original.trim() !== '') {
      texts.push({ target, id, field, original })
    }
  }
  add('document', document.id, 'intro_md', document.intro_md)
  add('document', document.id, 'summary_md', document.summary_md)
  for (const day of document.days) {
    add('day', day.id, 'title', day.title)
    add('day', day.id, 'notes_md', day.notes_md)
    for (const each of day.items) {
      if (each.kind === 'stay_anchor') {
        continue
      }
      add('item', each.id, 'name', each.name)
      add('item', each.id, 'description_md', each.description_md)
      add('item', each.id, 'story_md', each.story_md)
    }
    for (const leg of day.legs) {
      add('leg', leg.id, 'note', leg.note)
    }
  }
  return texts
}

/**
 * translationProgress counts how many of the texts translatableTexts lists
 * already have a translation into a language.
 */
export function translationProgress(document: TripDocument, lang: string): { done: number; total: number } {
  const texts = translatableTexts(document)
  const translated = document.translations?.[lang] ?? {}
  const done = texts.filter((text) => (translated[text.id]?.[text.field] ?? '') !== '').length
  return { done, total: texts.length }
}

/** translationOf reads one translated field, or '' when nobody translated it. */
export function translationOf(translations: DocumentTranslations | undefined, lang: string, id: string, field: string): string {
  return translations?.[lang]?.[id]?.[field] ?? ''
}

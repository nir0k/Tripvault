import type { PackingIcon, PackingItem } from '@/api/types'

// Helpers of a plan's packing list.

/** MAX_QUANTITY is the most of one thing an item may count, as the server allows. */
export const MAX_QUANTITY = 999

/**
 * SUGGESTED_CATEGORIES are the headings an empty list offers. They are keys of
 * the dictionaries, turned into the reader's words when chosen; from then on
 * a category is the trip's own text, renamed like any other.
 */
export const SUGGESTED_CATEGORIES = ['documents', 'clothes', 'hygiene', 'firstAid', 'electronics', 'gear'] as const

/** SUGGESTED_ICONS is the icon a suggested category starts with. */
export const SUGGESTED_ICONS: Record<(typeof SUGGESTED_CATEGORIES)[number], PackingIcon> = {
  documents: 'documents',
  clothes: 'clothes',
  hygiene: 'hygiene',
  firstAid: 'first_aid',
  electronics: 'electronics',
  gear: 'gear',
}

/**
 * parseQuickItem reads what was typed into the line that adds an item. A
 * count at the end - "Socks x3", "Socks ×3", "Socks *3" - becomes the
 * quantity, so a list is typed without leaving the keyboard.
 *
 * Returns:
 *   - the name and the quantity; an empty name when nothing but a count was typed.
 */
export function parseQuickItem(text: string): { name: string; quantity: number } {
  const trimmed = text.trim()
  const match = /^(.*?)\s*[x×*]\s*(\d{1,3})$/i.exec(trimmed)
  if (match && match[1] !== undefined && match[2] !== undefined) {
    const quantity = Number(match[2])
    if (quantity >= 1 && quantity <= MAX_QUANTITY && match[1].trim() !== '') {
      return { name: match[1].trim(), quantity }
    }
  }
  return { name: trimmed, quantity: 1 }
}

/** PackingSection is one category of the list as it is copied: its heading and its items. */
export interface PackingSection {
  title: string
  items: PackingItem[]
}

/**
 * packingText writes a packing list as plain text to paste into a chat: each
 * category under its name, each item a line of an unordered list with how
 * many, the note after a dash and who brings it in brackets. A category with
 * nothing in it is left out.
 *
 * Arguments:
 *   - sections: the categories in their order.
 *   - bringers: the names of the members who bring something, by identifier.
 *   - heading: a line above the list, such as the trip's name; null for none.
 *
 * Returns:
 *   - the text.
 */
export function packingText(sections: PackingSection[], bringers: Record<string, string>, heading: string | null): string {
  const blocks = sections
    .filter((section) => section.items.length > 0)
    .map((section) => [section.title, ...section.items.map((item) => {
      let line = `- ${item.name}`
      if (item.quantity > 1) {
        line += ` ×${item.quantity}`
      }
      if (item.note) {
        line += ` — ${item.note}`
      }
      const bringer = item.bringer_id ? bringers[item.bringer_id] : undefined
      if (bringer) {
        line += ` (${bringer})`
      }
      return line
    })].join('\n'))
  return [...(heading ? [heading] : []), ...blocks].join('\n\n')
}

import type { PackingAddCategory } from '@/api/packing'
import type { PackingIcon, PackingItem, PackingList, TagColor } from '@/api/types'

// Helpers of a plan's packing list.

/** MAX_QUANTITY is the most of one thing an item may count, as the server allows. */
export const MAX_QUANTITY = 999

/**
 * PackingTemplateItem is a thing of a template: a key of the dictionaries, how
 * many, and whether the dictionaries hold a note for it under the same key.
 */
interface PackingTemplateItem {
  key: string
  quantity?: number
  note?: boolean
}

/** PackingTemplateCategory is a category a template adds, with its look and its things. */
interface PackingTemplateCategory {
  key: string
  icon: PackingIcon
  // color is left out for the server to pick by the category's place.
  color?: TagColor
  items: PackingTemplateItem[]
}

/** PackingTemplate is a ready list offered to an editor, one category or several. */
export interface PackingTemplate {
  key: string
  icon: PackingIcon
  categories: PackingTemplateCategory[]
}

/**
 * PACKING_TEMPLATES are the lists an editor adds in one go. Their names are
 * keys of the dictionaries, written in the reader's words when added; from
 * then on they are the trip's own text, edited like any other. The hiking
 * template - a day out, with the night spent indoors - adds two categories,
 * named after it and in one colour, so they read as a pair.
 */
export const PACKING_TEMPLATES: readonly PackingTemplate[] = [
  {
    key: 'documents',
    icon: 'documents',
    categories: [{
      key: 'documents',
      icon: 'documents',
      items: ['passport', 'tickets', 'accommodation', 'insurance', 'drivingLicence', 'bankCards', 'cash', 'documentCopies']
        .map((key) => ({ key, note: key === 'insurance' })),
    }],
  },
  {
    key: 'firstAid',
    icon: 'first_aid',
    categories: [{
      key: 'firstAid',
      icon: 'first_aid',
      items: ['painkillers', 'antihistamines', 'stomach', 'plasters', 'blisterPlasters', 'elasticBandage', 'antiseptic',
        'personalMedicine'].map((key) => ({ key })),
    }],
  },
  {
    key: 'hygiene',
    icon: 'hygiene',
    categories: [{
      key: 'hygiene',
      icon: 'hygiene',
      items: ['toothbrush', 'deodorant', 'showerGel', 'comb', 'wetWipes', 'handSanitiser', 'sunscreen'].map((key) => ({ key })),
    }],
  },
  {
    key: 'electronics',
    icon: 'electronics',
    categories: [{
      key: 'electronics',
      icon: 'electronics',
      items: ['phone', 'powerBank', 'cables', 'camera', 'batteries'].map((key) => ({ key })),
    }],
  },
  {
    key: 'hiking',
    icon: 'hiking',
    categories: [
      {
        key: 'hikingGear',
        icon: 'gear',
        color: 'green',
        items: [
          { key: 'backpack' },
          { key: 'rainCover' },
          { key: 'trekkingPoles', note: true },
          { key: 'headlamp', note: true },
          { key: 'waterBottle', note: true },
          { key: 'maps', note: true },
          { key: 'whistle', note: true },
          { key: 'emergencyBlanket', note: true },
          { key: 'sunglasses' },
          { key: 'sitPad' },
          { key: 'dryBag', note: true },
          { key: 'snacks' },
        ],
      },
      {
        key: 'hikingClothes',
        icon: 'clothes',
        color: 'green',
        items: [
          { key: 'hikingBoots', note: true },
          { key: 'hikingSocks', quantity: 3, note: true },
          { key: 'baseLayer', note: true },
          { key: 'fleece', note: true },
          { key: 'rainJacket', note: true },
          { key: 'hikingTrousers', note: true },
          { key: 'tShirts', quantity: 2, note: true },
          { key: 'beanie', note: true },
          { key: 'buff', note: true },
          { key: 'gloves', note: true },
          { key: 'cap', note: true },
        ],
      },
    ],
  },
]

/**
 * templateSections writes a template in the reader's words, as the server
 * adds it.
 *
 * Arguments:
 *   - template: the template.
 *   - translate: turns a key of the dictionaries into the reader's words.
 *
 * Returns:
 *   - the categories with their things.
 */
export function templateSections(template: PackingTemplate, translate: (key: string) => string): PackingAddCategory[] {
  return template.categories.map((category) => ({
    name: translate(`packing.templateCategories.${category.key}`),
    icon: category.icon,
    ...(category.color ? { color: category.color } : {}),
    items: category.items.map((item) => ({
      name: translate(`packing.templateItems.${item.key}`),
      ...(item.quantity ? { quantity: item.quantity } : {}),
      ...(item.note ? { note: translate(`packing.templateNotes.${item.key}`) } : {}),
    })),
  }))
}

/**
 * templateAdded says whether a list holds a template whole: every category
 * under its name and every thing in it, in any case, as the server matches
 * them. Adding such a template again would add nothing.
 *
 * Arguments:
 *   - sections: the template in the reader's words (templateSections).
 *   - list: the trip's list.
 *   - locale: the language names are compared in.
 *
 * Returns:
 *   - true when nothing of the template is missing.
 */
export function templateAdded(sections: PackingAddCategory[], list: PackingList, locale: string): boolean {
  const fold = (text: string) => text.trim().toLocaleLowerCase(locale)
  return sections.every((section) => {
    const category = list.categories.find((candidate) => fold(candidate.name) === fold(section.name))
    if (!category) {
      return false
    }
    const names = new Set(list.items.filter((item) => item.category_id === category.id).map((item) => fold(item.name)))
    return section.items.every((item) => names.has(fold(item.name)))
  })
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

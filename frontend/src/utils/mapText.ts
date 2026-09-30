/**
 * mapText wraps text for a Leaflet tooltip or popup as an element holding it as
 * text. Leaflet writes a string it is given into the page as HTML, so a name
 * somebody typed - a station, a place - handed over as a string could carry
 * markup into every reader's map; an element's text is never read as markup.
 *
 * Arguments:
 *   - text: what the tooltip says.
 *
 * Returns:
 *   - a span holding the text.
 */
export function mapText(text: string): HTMLElement {
  const element = document.createElement('span')
  element.textContent = text
  return element
}

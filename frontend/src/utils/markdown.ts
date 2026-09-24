import DOMPurify from 'dompurify'
import { marked } from 'marked'

/**
 * renderMarkdown turns text a person wrote into safe HTML. Everything is
 * sanitised after rendering, so raw HTML in the text cannot run scripts, and
 * links open in a new tab without handing over the page.
 */
export function renderMarkdown(source: string): string {
  const html = marked.parse(source, { async: false, gfm: true, breaks: true })
  const clean = DOMPurify.sanitize(html, { USE_PROFILES: { html: true } })
  const template = document.createElement('template')
  template.innerHTML = clean
  for (const link of template.content.querySelectorAll('a[href]')) {
    link.setAttribute('target', '_blank')
    link.setAttribute('rel', 'noopener noreferrer nofollow')
  }
  return template.innerHTML
}

/**
 * markdownExcerpt - reads the opening of a text written in Markdown as plain
 * text, for a place too small to render it: the first sentences, cut at a
 * word when even those run long.
 *
 * Arguments:
 *   - source: the Markdown text.
 *   - sentences: how many sentences to keep.
 *   - limit: the most characters to keep.
 *
 * Returns:
 *   - the excerpt, ending in an ellipsis when a sentence had to be cut; empty
 *     for an empty text.
 */
export function markdownExcerpt(source: string, sentences = 2, limit = 220): string {
  const template = document.createElement('template')
  template.innerHTML = renderMarkdown(source)
  // Blocks are joined by a space, so a heading and the paragraph below it do
  // not run into one word.
  const text = [...template.content.childNodes]
    .map((node) => node.textContent ?? '')
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim()
  if (text === '') {
    return ''
  }
  const parts = text.match(/[^.!?…]+(?:[.!?…]+|$)/g) ?? [text]
  let excerpt = parts.slice(0, sentences).join('').trim()
  if (excerpt.length > limit) {
    const cut = excerpt.slice(0, limit)
    const space = cut.lastIndexOf(' ')
    excerpt = (space > limit / 2 ? cut.slice(0, space) : cut).replace(/[\s,;:.!?…-]+$/, '')
    return `${excerpt}…`
  }
  return excerpt
}

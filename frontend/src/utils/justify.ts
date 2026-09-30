import { MEDIA_SIZES, type MediaSize } from '@/api/media'

// The layout of a gallery the way Google Photos and Immich lay one out: rows
// of pictures in their own proportions, every row stretched to the width of
// the page and every picture in a row as tall as the others. Only the last row
// is left at the height asked for, so a few pictures at the end are not blown
// up to fill the line, and never taller than the row above it, so it does not
// stand out from the rows that were stretched.

/** JustifiedTile is where one picture goes: its index in the input and its size in pixels. */
interface JustifiedTile {
  index: number
  width: number
  height: number
}

/** JustifiedRow is one line of tiles, all of one height. */
export interface JustifiedRow {
  height: number
  tiles: JustifiedTile[]
}

/**
 * The narrowest and widest proportions a tile is given. A panorama or a
 * screenshot of a long page would otherwise take a row alone or shrink to a
 * sliver; beyond these it is cropped to fit its tile.
 */
const MIN_RATIO = 0.5
const MAX_RATIO = 3

/** tileRatio is a picture's width over its height, kept within the bounds above; 1 when unknown. */
export function tileRatio(width: number, height: number): number {
  if (width <= 0 || height <= 0) {
    return 1
  }
  return Math.min(Math.max(width / height, MIN_RATIO), MAX_RATIO)
}

/**
 * justifyRows lays pictures out in rows.
 *
 * A row takes pictures while they fit at the target height. The picture that
 * overflows it joins the row when the row is then squeezed less than it would
 * be stretched without it, so the rows stay as close to the target as they can.
 * Widths are whole pixels and the last tile of a full row takes what rounding
 * left over, so a row ends exactly at the edge.
 *
 * Arguments:
 *   - ratios: every picture's width over its height, in order.
 *   - width: the width of the page the rows fill.
 *   - target: the height a row aims for.
 *   - gap: the space between two tiles and between two rows.
 *
 * Returns:
 *   - the rows, each with its height and its tiles in order.
 */
export function justifyRows(ratios: number[], width: number, target: number, gap: number): JustifiedRow[] {
  const rows: JustifiedRow[] = []
  if (width <= 0 || target <= 0) {
    return rows
  }
  let start = 0
  while (start < ratios.length) {
    let sum = 0
    let end = start
    // Take pictures while the row at the target height is narrower than the page.
    while (end < ratios.length && (sum + ratios[end]!) * target + gap * (end - start) < width) {
      sum += ratios[end]!
      end++
    }
    if (end >= ratios.length) {
      rows.push(fixedRow(ratios, start, end, Math.min(target, rows.at(-1)?.height ?? target)))
      break
    }
    // The next picture overflows the row: keep it when that bends the row's
    // height less than leaving it out would, and always when the row is empty.
    const withIt = heightFor(sum + ratios[end]!, end - start + 1, width, gap)
    const without = end > start ? heightFor(sum, end - start, width, gap) : Infinity
    if (Math.abs(withIt - target) <= Math.abs(without - target)) {
      end++
    }
    rows.push(fullRow(ratios, start, end, width, gap))
    start = end
  }
  return rows
}

/** JustifiedStrip is a single row of pictures and how many of them it holds. */
export interface JustifiedStrip {
  row: JustifiedRow
  /**
   * How many pictures the row lays out, from the first. When some are left
   * out the row ends with one more tile, at index shown, that stands for them.
   */
  shown: number
}

/**
 * justifyStrip lays the first pictures out in a single row, the way a day or a
 * place of a report shows a few of its pictures.
 *
 * While every picture allowed fits at the target height they are laid out at
 * it and the row is left as it falls; when some of them are left out the row
 * ends with a tile standing for the rest. When the row is full it is stretched
 * across the width, taking the picture that overflows it when that bends the
 * height less than leaving it out, as justifyRows does. A row holds at least
 * one picture however narrow the page is.
 *
 * Arguments:
 *   - ratios: every picture's width over its height, in order.
 *   - width: the width of the page the row fills.
 *   - target: the height the row aims for.
 *   - gap: the space between two tiles.
 *   - limit: the most pictures the row may lay out; at least one.
 *   - moreRatio: the proportions of the tile standing for the rest.
 *
 * Returns:
 *   - the row, its last tile the one for the rest when shown is less than
 *     the number of pictures; null when there is nothing to lay out.
 */
export function justifyStrip(ratios: number[], width: number, target: number, gap: number,
  limit: number, moreRatio: number): JustifiedStrip | null {
  const total = ratios.length
  const allowed = Math.min(total, Math.max(limit, 1))
  if (width <= 0 || target <= 0 || total === 0) {
    return null
  }
  // cellsOf lays the first count pictures out, followed by the tile for the rest
  // unless there is no rest.
  const cellsOf = (count: number): number[] =>
    count < total ? [...ratios.slice(0, count), moreRatio] : ratios.slice(0, count)
  const fits = (count: number): boolean => {
    const cells = cellsOf(count)
    const sum = cells.reduce((acc, ratio) => acc + ratio, 0)
    return sum * target + gap * (cells.length - 1) <= width
  }

  let count = 0
  while (count < allowed && fits(count + 1)) {
    count++
  }
  if (count === allowed && count > 0) {
    const cells = cellsOf(count)
    return { row: fixedRow(cells, 0, cells.length, target), shown: count }
  }
  // The next picture overflows the row: keep it when that bends the row's
  // height less than leaving it out would, and always when the row is empty.
  let shown = count + 1
  if (count > 0) {
    const heightOf = (cells: number[]): number =>
      heightFor(cells.reduce((acc, ratio) => acc + ratio, 0), cells.length, width, gap)
    if (Math.abs(heightOf(cellsOf(count)) - target) < Math.abs(heightOf(cellsOf(count + 1)) - target)) {
      shown = count
    }
  }
  const cells = cellsOf(shown)
  return { row: fullRow(cells, 0, cells.length, width, gap), shown }
}

/** heightFor is the height at which count tiles of the given ratio sum fill the width. */
function heightFor(sum: number, count: number, width: number, gap: number): number {
  return (width - gap * (count - 1)) / sum
}

/** fullRow stretches the tiles from start to end, exclusive, across the whole width. */
function fullRow(ratios: number[], start: number, end: number, width: number, gap: number): JustifiedRow {
  let sum = 0
  for (let index = start; index < end; index++) {
    sum += ratios[index]!
  }
  const height = Math.round(heightFor(sum, end - start, width, gap))
  const tiles: JustifiedTile[] = []
  let used = 0
  for (let index = start; index < end; index++) {
    const last = index === end - 1
    const tileWidth = last
      ? Math.max(Math.floor(width - gap * (end - start - 1) - used), 1)
      : Math.max(Math.floor(ratios[index]! * height), 1)
    used += tileWidth
    tiles.push({ index, width: tileWidth, height })
  }
  return { height, tiles }
}

/** fixedRow lays the last tiles out at the given height, left as they fall. */
function fixedRow(ratios: number[], start: number, end: number, target: number): JustifiedRow {
  const height = Math.round(target)
  const tiles: JustifiedTile[] = []
  for (let index = start; index < end; index++) {
    tiles.push({ index, width: Math.max(Math.round(ratios[index]! * height), 1), height })
  }
  return { height, tiles }
}

/**
 * previewSize picks the preview a tile is shown with: the narrowest served
 * width that still covers the tile on this screen. Only a few widths are
 * served, so a window resized by a few pixels keeps the preview it has.
 *
 * Arguments:
 *   - width: the tile's width in CSS pixels.
 *   - density: the screen's device pixels per CSS pixel.
 *
 * Returns:
 *   - the preview width to fetch; the widest tile preview when none is enough.
 */
export function previewSize(width: number, density: number): MediaSize {
  const needed = width * Math.max(density, 1)
  const offered = MEDIA_SIZES.filter((size) => size <= 1280)
  return offered.find((size) => size >= needed) ?? offered[offered.length - 1]!
}

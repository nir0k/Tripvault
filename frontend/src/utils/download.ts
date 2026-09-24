/**
 * Saving a file the API served.
 *
 * Every download here is authenticated, so a plain link will not do: the file is
 * fetched with the session's token and handed to the browser as a blob.
 */

/**
 * saveBlob hands a fetched file to the browser's own download.
 *
 * The object URL is released straight away: the click has already started the
 * save, and holding the blob after that only keeps it in memory.
 */
export function saveBlob(body: Blob, name: string): void {
  const url = URL.createObjectURL(body)
  try {
    const link = document.createElement('a')
    link.href = url
    link.download = name
    link.click()
  } finally {
    URL.revokeObjectURL(url)
  }
}

/**
 * filenameFrom reads the name out of a Content-Disposition header.
 *
 * A download with no name at all is saved as "download" by the browser, which
 * tells its owner nothing, so a fallback is always given.
 */
export function filenameFrom(header: unknown, fallback: string): string {
  const match = typeof header === 'string' ? /filename="([^"]+)"/.exec(header) : null
  return match?.[1] ?? fallback
}

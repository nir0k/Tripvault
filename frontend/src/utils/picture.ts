// Shrinking a photograph before it is uploaded. A phone takes pictures far
// larger than a report ever shows, and the difference is bytes the service
// stores and sends for nothing; the long side of 2560 pixels still fills any
// screen a report is read on.

/** MAX_UPLOAD_EDGE is the longest side a shrunk picture keeps, in pixels. */
const MAX_UPLOAD_EDGE = 2560

/** SHRINK_QUALITY is the JPEG quality a shrunk picture is written at. */
const SHRINK_QUALITY = 0.85

/**
 * shrinkPicture - reduces a picture to MAX_UPLOAD_EDGE on its long side.
 *
 * A file that is already small enough, or one the browser cannot decode, is
 * returned untouched: the upload is worth more than the saving, and the server
 * checks what it is given anyway.
 *
 * Arguments:
 *   - file: the chosen file.
 *
 * Returns:
 *   - the file to upload, which is the original when nothing was gained.
 */
export async function shrinkPicture(file: File): Promise<File> {
  if (!file.type.startsWith('image/')) {
    return file
  }
  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file)
  } catch {
    return file
  }
  try {
    const longest = Math.max(bitmap.width, bitmap.height)
    if (longest <= MAX_UPLOAD_EDGE) {
      return file
    }
    const scale = MAX_UPLOAD_EDGE / longest
    const canvas = document.createElement('canvas')
    canvas.width = Math.round(bitmap.width * scale)
    canvas.height = Math.round(bitmap.height * scale)
    const context = canvas.getContext('2d')
    if (!context) {
      return file
    }
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height)

    const blob = await new Promise<Blob | null>((resolve) => {
      canvas.toBlob(resolve, 'image/jpeg', SHRINK_QUALITY)
    })
    // A picture that came out larger than it went in is not worth replacing.
    if (!blob || blob.size >= file.size) {
      return file
    }
    return new File([blob], renameToJPEG(file.name), { type: 'image/jpeg', lastModified: file.lastModified })
  } finally {
    bitmap.close()
  }
}

/** renameToJPEG gives a converted picture an extension that matches it. */
function renameToJPEG(name: string): string {
  const dot = name.lastIndexOf('.')
  return (dot > 0 ? name.slice(0, dot) : name) + '.jpg'
}

/**
 * fileChecksum - reads the SHA-256 of a file, in hex.
 *
 * The server stores a picture once per trip and knows a copy by this sum of
 * the original as well as by the sum of what was sent, which shrinking makes
 * different each time the setting changes. The browser offers the digest only
 * over HTTPS and on localhost; elsewhere there is none, and the server compares
 * the bytes it received alone.
 *
 * Arguments:
 *   - file: the file as it was chosen.
 *
 * Returns:
 *   - the sum in lower-case hex, or null when the browser cannot compute it.
 */
export async function fileChecksum(file: File): Promise<string | null> {
  if (!globalThis.crypto?.subtle) {
    return null
  }
  try {
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer())
    return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('')
  } catch {
    return null
  }
}

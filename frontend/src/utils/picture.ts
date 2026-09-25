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
    // A canvas writes a JPEG with no metadata, and the server reads when and
    // where a picture was taken from it: the gallery's order and the hints
    // that place an upload on its day come from there. So the original's EXIF
    // is carried over into the smaller file.
    const head = file.type === 'image/jpeg' ? await file.slice(0, EXIF_SEARCH_BYTES).arrayBuffer() : null
    const exif = head ? exifSegment(new Uint8Array(head)) : null
    const shrunk = exif ? withExif(new Uint8Array(await blob.arrayBuffer()), exif) : blob
    return new File([shrunk], renameToJPEG(file.name), { type: 'image/jpeg', lastModified: file.lastModified })
  } finally {
    bitmap.close()
  }
}

/**
 * EXIF_SEARCH_BYTES is how much of the start of a JPEG is read for its EXIF,
 * which sits before the picture data in a segment of at most 64 KiB.
 */
const EXIF_SEARCH_BYTES = 256 * 1024

/** EXIF_ORIENTATION is the tag that says how a picture is to be turned. */
const EXIF_ORIENTATION = 0x0112

/**
 * exifSegment - finds the EXIF segment of a JPEG and copies it, marked upright.
 *
 * The browser has already turned the picture as the EXIF says when it drew it
 * on the canvas, so the copy's orientation is set to 1; left as it was, the
 * server would turn the smaller picture a second time.
 *
 * Arguments:
 *   - jpeg: the start of a JPEG file.
 *
 * Returns:
 *   - the whole APP1 segment, marker and length included, or null when the
 *     file is not a JPEG or carries no EXIF.
 */
export function exifSegment(jpeg: Uint8Array): Uint8Array | null {
  if (jpeg[0] !== 0xff || jpeg[1] !== 0xd8) {
    return null
  }
  let offset = 2
  while (offset + 4 <= jpeg.length && jpeg[offset] === 0xff) {
    const marker = jpeg[offset + 1] ?? 0
    // The picture data starts here; nothing after it is metadata.
    if (marker === 0xda || marker === 0xd9) {
      return null
    }
    const length = ((jpeg[offset + 2] ?? 0) << 8) | (jpeg[offset + 3] ?? 0)
    const end = offset + 2 + length
    const header = String.fromCharCode(...jpeg.subarray(offset + 4, offset + 10))
    if (marker === 0xe1 && header === 'Exif\0\0' && end <= jpeg.length) {
      const segment = jpeg.slice(offset, end)
      markUpright(segment)
      return segment
    }
    offset = end
  }
  return null
}

// markUpright sets the orientation of the first image directory of an APP1
// segment to 1, in the byte order the segment declares. A segment it cannot
// read is left alone.
function markUpright(segment: Uint8Array): void {
  const tiff = 10
  const view = new DataView(segment.buffer, segment.byteOffset, segment.byteLength)
  if (tiff + 8 > view.byteLength) {
    return
  }
  const little = view.getUint16(tiff) === 0x4949
  const directory = tiff + view.getUint32(tiff + 4, little)
  if (directory + 2 > view.byteLength) {
    return
  }
  const count = view.getUint16(directory, little)
  for (let index = 0; index < count; index++) {
    const entry = directory + 2 + index * 12
    if (entry + 12 > view.byteLength) {
      return
    }
    // The orientation is a SHORT, kept in the first two bytes of the value.
    if (view.getUint16(entry, little) === EXIF_ORIENTATION && view.getUint16(entry + 2, little) === 3) {
      view.setUint16(entry + 8, 1, little)
      return
    }
  }
}

/**
 * withExif - puts an EXIF segment into a JPEG right after its start marker,
 * where readers look for it.
 *
 * Arguments:
 *   - jpeg: the JPEG a canvas wrote, which has no EXIF of its own.
 *   - segment: the APP1 segment exifSegment copied.
 *
 * Returns:
 *   - the JPEG with the segment in it.
 */
export function withExif(jpeg: Uint8Array, segment: Uint8Array): Uint8Array<ArrayBuffer> {
  const joined = new Uint8Array(jpeg.length + segment.length)
  joined.set(jpeg.subarray(0, 2))
  joined.set(segment, 2)
  joined.set(jpeg.subarray(2), 2 + segment.length)
  return joined
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

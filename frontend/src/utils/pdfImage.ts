import { getDocument, GlobalWorkerOptions } from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'

/**
 * Turning a document the server wrote into a picture.
 *
 * The picture is the PDF itself, drawn by pdf.js, so it reads exactly like the
 * printed list and no second layout has to be kept. This module is loaded only
 * when a picture is asked for, and pdf.js with it; its worker is served by the
 * application, never by a CDN.
 */

GlobalWorkerOptions.workerSrc = workerUrl

/** IMAGE_DPI is how finely a page is drawn: sharp on a phone, light enough to send. */
const IMAGE_DPI = 200
/** JPEG_QUALITY trades the size of the file against the edges of the type. */
const JPEG_QUALITY = 0.9
/** POINTS_PER_INCH is the unit a PDF measures its pages in. */
const POINTS_PER_INCH = 72

/**
 * pdfToJpeg draws every page of a PDF, one under the other, into one JPEG.
 *
 * Arguments:
 *   - pdf: the document.
 *
 * Returns:
 *   - the picture.
 */
export async function pdfToJpeg(pdf: Blob): Promise<Blob> {
  const loading = getDocument({ data: new Uint8Array(await pdf.arrayBuffer()) })
  try {
    const document = await loading.promise
    const pages: HTMLCanvasElement[] = []
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number)
      const viewport = page.getViewport({ scale: IMAGE_DPI / POINTS_PER_INCH })
      const canvas = window.document.createElement('canvas')
      canvas.width = Math.ceil(viewport.width)
      canvas.height = Math.ceil(viewport.height)
      await page.render({ canvas, viewport }).promise
      pages.push(canvas)
    }

    const sheet = window.document.createElement('canvas')
    sheet.width = Math.max(...pages.map((page) => page.width))
    sheet.height = pages.reduce((height, page) => height + page.height, 0)
    const context = sheet.getContext('2d')
    if (!context) {
      throw new Error('canvas is unavailable')
    }
    // A JPEG has no transparency; whatever a page leaves bare is white paper.
    context.fillStyle = '#ffffff'
    context.fillRect(0, 0, sheet.width, sheet.height)
    let top = 0
    for (const page of pages) {
      context.drawImage(page, 0, top)
      top += page.height
    }
    return await new Promise<Blob>((resolve, reject) => {
      sheet.toBlob((blob) => (blob ? resolve(blob) : reject(new Error('the picture could not be encoded'))), 'image/jpeg', JPEG_QUALITY)
    })
  } finally {
    await loading.destroy()
  }
}

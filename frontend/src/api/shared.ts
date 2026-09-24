import { http, PDF_TIMEOUT_MS } from './client'
import { filenameFrom, saveBlob } from '@/utils/download'
import type { Shared, TripDocument, Units } from './types'

// Reading by a read-only link. These are the only endpoints the share token is
// sent to; the client adds the header, so nothing here handles the token itself.

/** getShared reads which trip the tab's link opens, and what may be read of it. */
export async function getShared(): Promise<Shared> {
  return (await http.get<Shared>('/api/v1/shared')).data
}

/** getSharedDocument reads the one document the link opens, its plan or its report. */
export async function getSharedDocument(): Promise<TripDocument> {
  return (await http.get<TripDocument>('/api/v1/shared/document')).data
}

/**
 * downloadSharedReportPDF saves the shared report as a PDF.
 *
 * The language and the units travel with the request: a link has no account
 * behind it for the server to take them from, so the page sends what it is
 * showing the reader - contentLang being the language the report's own words
 * are read in.
 */
export async function downloadSharedReportPDF(
  language: string,
  units: Units,
  photos: boolean,
  contentLang?: string,
): Promise<void> {
  const response = await http.get<Blob>('/api/v1/shared/report/pdf', {
    params: {
      lang: language, units, ...(photos ? {} : { photos: 'false' }), ...(contentLang ? { content_lang: contentLang } : {}),
    },
    responseType: 'blob',
    timeout: PDF_TIMEOUT_MS,
  })
  saveBlob(response.data, filenameFrom(response.headers['content-disposition'], 'report.pdf'))
}

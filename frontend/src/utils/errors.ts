import { ApiError } from '@/api/client'

type Translate = (key: string, named?: Record<string, unknown>) => string
type HasTranslation = (key: string) => boolean

/**
 * errorMessage turns a failed call into a sentence in the reader's language.
 *
 * The backend sends stable codes rather than prose. A validation failure is
 * described by its reason, a known code by its own message, and anything else
 * by a generic one, so a new server code never shows up as a raw key.
 */
export function errorMessage(error: unknown, t: Translate, te: HasTranslation): string {
  if (!(error instanceof ApiError)) {
    return t('errors.unknown_error')
  }
  if (error.code === 'validation_failed') {
    const reason = String(error.details.reason ?? '')
    if (reason && te(`errors.validation.${reason}`)) {
      return t(`errors.validation.${reason}`)
    }
  }
  if (error.code === 'too_many_attempts') {
    const seconds = Number(error.details.retry_after ?? 0)
    return t('errors.too_many_attempts', { minutes: Math.max(1, Math.ceil(seconds / 60)) })
  }
  if (te(`errors.${error.code}`)) {
    return t(`errors.${error.code}`)
  }
  return t('errors.unknown_error')
}

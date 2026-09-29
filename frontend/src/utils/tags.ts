import { ApiError } from '@/api/client'
import { errorMessage } from '@/utils/errors'

type Translate = (key: string, named?: Record<string, unknown>) => string
type HasTranslation = (key: string) => boolean

/**
 * tagError explains a failed change to the reader's tags. A name already in
 * use is said in the words of tags: the generic message of that code speaks of
 * accounts.
 */
export function tagError(error: unknown, t: Translate, te: HasTranslation): string {
  if (error instanceof ApiError && error.code === 'already_exists') {
    return t('tags.exists')
  }
  return errorMessage(error, t, te)
}

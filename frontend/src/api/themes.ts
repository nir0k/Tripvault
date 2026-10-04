import { http } from './client'
import type { InstanceTheme, ListResponse, ThemeFile } from './types'

/** themePath builds the administration path of a theme. */
function themePath(themeId: string): string {
  return `/api/v1/admin/themes/${encodeURIComponent(themeId)}`
}

/** listThemes returns the instance's colour themes by name, with their palettes. */
export async function listThemes(): Promise<InstanceTheme[]> {
  return (await http.get<ListResponse<InstanceTheme>>('/api/v1/themes')).data.items
}

/** createTheme uploads a theme file; a name another theme has answers 409. */
export async function createTheme(file: ThemeFile): Promise<InstanceTheme> {
  return (await http.post<InstanceTheme>('/api/v1/admin/themes', file)).data
}

/** replaceTheme puts a new file in place of a theme, which its readers keep. */
export async function replaceTheme(themeId: string, file: ThemeFile): Promise<InstanceTheme> {
  return (await http.put<InstanceTheme>(themePath(themeId), file)).data
}

/** deleteTheme removes a theme; its readers go back to the built-in one. */
export async function deleteTheme(themeId: string): Promise<void> {
  await http.delete(themePath(themeId))
}

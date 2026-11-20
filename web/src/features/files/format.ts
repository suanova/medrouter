// Shared formatting for the file browser and the preview dialog.

export function formatBytes(value: number | undefined, locale: string): string {
  if (value == null) return '—'
  if (value < 1024) return `${value} B`
  const units = ['KB', 'MB', 'GB', 'TB', 'PB']
  let size = value / 1024
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${new Intl.NumberFormat(locale === 'zh' ? 'zh-CN' : 'en', {
    maximumFractionDigits: size >= 10 ? 0 : 1,
  }).format(size)} ${units[index]}`
}

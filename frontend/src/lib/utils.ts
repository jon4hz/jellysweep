export function formatFileSize(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const size = bytes / Math.pow(1024, i)
  return `${size.toFixed(i > 0 ? 1 : 0)} ${units[i]!}`
}

export function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  })
}

export function formatExactDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('en-US', {
    month: 'long',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    timeZoneName: 'short',
  })
}

export function timeUntil(dateStr: string): string {
  const now = new Date()
  const target = new Date(dateStr)
  const diffMs = target.getTime() - now.getTime()

  if (diffMs < 0) return 'overdue'

  const days = Math.floor(diffMs / (1000 * 60 * 60 * 24))
  if (days > 30) return `${Math.floor(days / 30)} months`
  if (days > 0) return `${days} day${days === 1 ? '' : 's'}`

  const hours = Math.floor(diffMs / (1000 * 60 * 60))
  if (hours > 0) return `${hours} hour${hours === 1 ? '' : 's'}`

  const minutes = Math.floor(diffMs / (1000 * 60))
  return `${minutes} min${minutes === 1 ? '' : 's'}`
}

export function debounce<T extends (...args: Parameters<T>) => void>(fn: T, ms: number): (...args: Parameters<T>) => void {
  let timer: ReturnType<typeof setTimeout>
  return (...args: Parameters<T>) => {
    clearTimeout(timer)
    timer = setTimeout(() => fn(...args), ms)
  }
}

export function getEpisodeTooltip(keepCount: number): string {
  if (keepCount <= 0) return 'Unless requested otherwise, Jellysweep will delete all episodes.'
  if (keepCount === 1) return 'Unless requested otherwise, Jellysweep will delete everything except the first episode.'
  const words: Record<number, string> = { 2: 'two', 3: 'three', 4: 'four', 5: 'five' }
  const count = words[keepCount] ?? String(keepCount)
  return `Unless requested otherwise, Jellysweep will delete everything except the first ${count} episodes.`
}

export function getSeasonTooltip(keepCount: number): string {
  if (keepCount <= 0) return 'Unless requested otherwise, Jellysweep will delete all seasons.'
  if (keepCount === 1) return 'Unless requested otherwise, Jellysweep will delete everything except the first season.'
  const words: Record<number, string> = { 2: 'two', 3: 'three', 4: 'four', 5: 'five' }
  const count = words[keepCount] ?? String(keepCount)
  return `Unless requested otherwise, Jellysweep will delete everything except the first ${count} seasons.`
}

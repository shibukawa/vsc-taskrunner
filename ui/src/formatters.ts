export function formatDateTime(value?: string): string {
  const date = parseDisplayDate(value)
  if (!date) {
    return 'N/A'
  }
  return new Intl.DateTimeFormat(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function formatFullDateTime(value?: string): string {
  const date = parseDisplayDate(value)
  if (!date) {
    return 'N/A'
  }
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}

export function formatRelativeTime(value?: string): string {
  const date = parseDisplayDate(value)
  if (!date) {
    return 'N/A'
  }
  const timestamp = date.getTime()

  const deltaSeconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  if (deltaSeconds < 10) {
    return 'just now'
  }
  if (deltaSeconds < 60) {
    return `${deltaSeconds}s ago`
  }

  const deltaMinutes = Math.floor(deltaSeconds / 60)
  if (deltaMinutes < 60) {
    return `${deltaMinutes} min ago`
  }

  const deltaHours = Math.floor(deltaMinutes / 60)
  if (deltaHours < 24) {
    return `${deltaHours} hour${deltaHours === 1 ? '' : 's'} ago`
  }

  const deltaDays = Math.floor(deltaHours / 24)
  if (deltaDays < 30) {
    return `${deltaDays} day${deltaDays === 1 ? '' : 's'} ago`
  }

  const deltaMonths = Math.floor(deltaDays / 30)
  if (deltaMonths < 12) {
    return `${deltaMonths} month${deltaMonths === 1 ? '' : 's'} ago`
  }

  const deltaYears = Math.floor(deltaMonths / 12)
  return `${deltaYears} year${deltaYears === 1 ? '' : 's'} ago`
}

export function formatDuration(start?: string, end?: string): string {
  if (!start) {
    return 'N/A'
  }
  const startTime = new Date(start).getTime()
  const endTime = end ? new Date(end).getTime() : Date.now()
  if (Number.isNaN(startTime) || Number.isNaN(endTime)) {
    return 'N/A'
  }
  const seconds = Math.max(0, Math.round((endTime - startTime) / 1000))
  if (seconds < 60) {
    return `${seconds}s`
  }
  const minutes = Math.floor(seconds / 60)
  const remain = seconds % 60
  return `${minutes}m ${remain}s`
}

export function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index += 1
  }
  return `${value.toFixed(value >= 100 || index === 0 ? 0 : 1)} ${units[index]}`
}

export function prettyStatus(status: string): string {
  return status.replace('-', ' ')
}

export function statusClass(status: string): string {
  return `status-${status}`
}

function parseDisplayDate(value?: string): Date | null {
  if (!value) {
    return null
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return null
  }
  if (date.getUTCFullYear() <= 1) {
    return null
  }
  return date
}

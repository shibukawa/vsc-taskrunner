import type { ANSIStyleState, LogSegment } from './types'

const ANSI_ESCAPE_PATTERN = /\x1b\[([0-9;]*)m/g
const ANSI_COLORS = ['#111827', '#ff6b6b', '#8be28b', '#f4d35e', '#7dc3ff', '#c792ea', '#72e6d1', '#d6deeb']
const ANSI_BRIGHT_COLORS = ['#6b7280', '#ff8787', '#b9f27c', '#ffe082', '#9ecfff', '#ddb3ff', '#98f5e1', '#ffffff']

function defaultANSIState(): ANSIStyleState {
  return {
    fg: null,
    bg: null,
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    strike: false,
    inverse: false,
  }
}

export function parseAnsiToSegments(value: string): LogSegment[] {
  const plain = normalizeTerminalContent(value)
  if (!plain) {
    return []
  }
  const segments: LogSegment[] = []
  const state = defaultANSIState()
  let cursor = 0
  for (const match of plain.matchAll(ANSI_ESCAPE_PATTERN)) {
    const index = match.index ?? 0
    if (index > cursor) {
      pushSegment(segments, plain.slice(cursor, index), state)
    }
    applyANSIStyles(state, parseANSISequence(match[1] ?? '0'))
    cursor = index + match[0].length
  }
  if (cursor < plain.length) {
    pushSegment(segments, plain.slice(cursor), state)
  }
  return segments
}

function normalizeTerminalContent(value: string): string {
  if (!value) {
    return ''
  }
  let currentLine = ''
  let result = ''
  let index = 0
  while (index < value.length) {
    const char = value[index]
    if (char === '\u001B') {
      const sequence = readEscapeSequence(value, index)
      if (!sequence) {
        index += 1
        continue
      }
      if (sequence.kind === 'erase-line' || sequence.kind === 'cursor-horizontal-absolute') {
        currentLine = ''
      } else if (sequence.kind === 'sgr') {
        result += sequence.raw
      }
      index = sequence.nextIndex
      continue
    }
    if (char === '\r') {
      currentLine = ''
      index += 1
      continue
    }
    currentLine += char
    result += char
    index += 1
  }
  return result
}

function readEscapeSequence(value: string, start: number) {
  if (value[start + 1] !== '[') {
    return null
  }
  let end = start + 2
  while (end < value.length) {
    const char = value[end]
    if ((char >= '@' && char <= '~') || char === 'm') {
      const raw = value.slice(start, end + 1)
      const finalChar = value[end]
      if (finalChar === 'm') {
        return { kind: 'sgr', raw, nextIndex: end + 1 }
      }
      if (finalChar === 'K') {
        return { kind: 'erase-line', raw, nextIndex: end + 1 }
      }
      if (finalChar === 'G') {
        return { kind: 'cursor-horizontal-absolute', raw, nextIndex: end + 1 }
      }
      return { kind: 'other', raw, nextIndex: end + 1 }
    }
    end += 1
  }
  return null
}

function parseANSISequence(raw: string): number[] {
  return raw.split(';').map((part) => {
    const value = Number.parseInt(part || '0', 10)
    return Number.isNaN(value) ? 0 : value
  })
}

function pushSegment(segments: LogSegment[], text: string, state: ANSIStyleState) {
  if (!text) {
    return
  }
  segments.push({
    text,
    class: buildANSIClasses(state),
    style: buildANSIStyle(state),
  })
}

function buildANSIClasses(state: ANSIStyleState): string[] {
  const classes = ['terminal-segment']
  if (state.bold) {
    classes.push('ansi-bold')
  }
  if (state.dim) {
    classes.push('ansi-dim')
  }
  if (state.italic) {
    classes.push('ansi-italic')
  }
  if (state.underline) {
    classes.push('ansi-underline')
  }
  if (state.strike) {
    classes.push('ansi-strike')
  }
  return classes
}

function buildANSIStyle(state: ANSIStyleState): Record<string, string> {
  const style: Record<string, string> = {}
  const fg = state.inverse ? state.bg : state.fg
  const bg = state.inverse ? state.fg : state.bg
  if (fg) {
    style.color = fg
  }
  if (bg) {
    style.backgroundColor = bg
  }
  return style
}

function applyANSIStyles(state: ANSIStyleState, codes: number[]) {
  const nextCodes = codes.length === 0 ? [0] : codes
  for (let index = 0; index < nextCodes.length; index += 1) {
    const code = nextCodes[index]
    switch (code) {
      case 0:
        Object.assign(state, defaultANSIState())
        break
      case 1:
        state.bold = true
        break
      case 2:
        state.dim = true
        break
      case 3:
        state.italic = true
        break
      case 4:
        state.underline = true
        break
      case 9:
        state.strike = true
        break
      case 22:
        state.bold = false
        state.dim = false
        break
      case 23:
        state.italic = false
        break
      case 24:
        state.underline = false
        break
      case 29:
        state.strike = false
        break
      case 7:
        state.inverse = true
        break
      case 27:
        state.inverse = false
        break
      case 39:
        state.fg = null
        break
      case 49:
        state.bg = null
        break
      default:
        if (code >= 30 && code <= 37) {
          state.fg = ANSI_COLORS[code - 30]
        } else if (code >= 40 && code <= 47) {
          state.bg = ANSI_COLORS[code - 40]
        } else if (code >= 90 && code <= 97) {
          state.fg = ANSI_BRIGHT_COLORS[code - 90]
        } else if (code >= 100 && code <= 107) {
          state.bg = ANSI_BRIGHT_COLORS[code - 100]
        } else if (code === 38 || code === 48) {
          const isForeground = code === 38
          const mode = nextCodes[index + 1]
          if (mode === 5 && nextCodes[index + 2] !== undefined) {
            const color = ansi256Color(nextCodes[index + 2])
            if (isForeground) {
              state.fg = color
            } else {
              state.bg = color
            }
            index += 2
          } else if (mode === 2 && nextCodes[index + 4] !== undefined) {
            const color = rgbToHex(nextCodes[index + 2], nextCodes[index + 3], nextCodes[index + 4])
            if (isForeground) {
              state.fg = color
            } else {
              state.bg = color
            }
            index += 4
          }
        }
        break
    }
  }
}

function ansi256Color(code: number): string {
  if (code < 8) {
    return ANSI_COLORS[code]
  }
  if (code < 16) {
    return ANSI_BRIGHT_COLORS[code - 8]
  }
  if (code >= 232) {
    const level = (code - 232) * 10 + 8
    return rgbToHex(level, level, level)
  }
  const normalized = code - 16
  const red = Math.floor(normalized / 36)
  const green = Math.floor((normalized % 36) / 6)
  const blue = normalized % 6
  const convert = (value: number) => (value === 0 ? 0 : value * 40 + 55)
  return rgbToHex(convert(red), convert(green), convert(blue))
}

function rgbToHex(red: number, green: number, blue: number): string {
  return `#${[red, green, blue].map((value) => value.toString(16).padStart(2, '0')).join('')}`
}

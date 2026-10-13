// RichEditor: a dependency-free formatted message box. It is a contenteditable
// area with a small toolbar and keyboard shortcuts; what you see is what gets
// sent, as Markdown (which is what messages are stored and rendered as).
import { forwardRef, useImperativeHandle, useRef, type KeyboardEvent, type ReactNode } from 'react'
import { Bold, Code, Italic, Link2, List, ListOrdered, Quote, Strikethrough } from 'lucide-react'
import { cx } from '../ui'

export interface RichEditorHandle {
  focus(): void
  clear(): void
  isEmpty(): boolean
  getMarkdown(): string
  /** Plain text from the start of the box to the caret (for @mention lookups). */
  textBeforeCaret(): string
  /** Replace the `count` characters before the caret with `text`. */
  replaceBeforeCaret(count: number, text: string): void
}

interface Props {
  placeholder: string
  toolbar: boolean
  onInput: () => void
  onSubmit: () => void
  onFiles: (files: File[]) => void
  /** Return true to swallow the key (used by the mention list). */
  onKeyDown?: (e: KeyboardEvent<HTMLDivElement>) => boolean
  onBlur?: () => void
  onClick?: () => void
}

const BLOCK = new Set(['DIV', 'P', 'UL', 'OL', 'BLOCKQUOTE', 'PRE', 'LI', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6'])

// --- DOM → Markdown -------------------------------------------------------

function wrap(s: string, mark: string): string {
  if (!s.trim()) return s
  const lead = /^\s*/.exec(s)![0]
  const trail = /\s*$/.exec(s)![0]
  return lead + mark + s.trim() + mark + trail
}

function mdChildren(el: Node): string {
  let out = ''
  el.childNodes.forEach((c) => {
    const isBlock = c.nodeType === Node.ELEMENT_NODE && BLOCK.has((c as HTMLElement).tagName)
    let s = mdNode(c)
    if (isBlock) {
      if (out && !out.endsWith('\n')) out += '\n'
      s = s.replace(/^\n+/, '')
      if (!s.endsWith('\n')) s += '\n'
    }
    out += s
  })
  return out
}

function mdNode(n: Node): string {
  if (n.nodeType === Node.TEXT_NODE) return (n.textContent ?? '').replace(/ /g, ' ')
  if (n.nodeType !== Node.ELEMENT_NODE) return ''
  const el = n as HTMLElement
  switch (el.tagName) {
    case 'BR':
      return '\n'
    case 'B':
    case 'STRONG':
      return wrap(mdChildren(el), '**')
    case 'I':
    case 'EM':
      return wrap(mdChildren(el), '*')
    case 'S':
    case 'STRIKE':
    case 'DEL':
      return wrap(mdChildren(el), '~~')
    case 'CODE': {
      if (el.closest('pre')) return el.textContent ?? ''
      const t = (el.textContent ?? '').replace(/​/g, '')
      return t ? '`' + t + '`' : ''
    }
    case 'PRE':
      return '```\n' + (el.textContent ?? '').replace(/\n$/, '') + '\n```'
    case 'A': {
      const inner = mdChildren(el)
      const href = el.getAttribute('href') ?? ''
      return href && inner.trim() ? `[${inner.trim()}](${href})` : inner
    }
    case 'UL':
    case 'OL': {
      const lines: string[] = []
      let i = 0
      el.childNodes.forEach((c) => {
        if ((c as HTMLElement).tagName !== 'LI') return
        i++
        const body = mdChildren(c).replace(/^\n+|\n+$/g, '')
        lines.push((el.tagName === 'OL' ? `${i}. ` : '- ') + body)
      })
      return lines.join('\n')
    }
    case 'BLOCKQUOTE': {
      const body = mdChildren(el).replace(/^\n+|\n+$/g, '')
      return body
        .split('\n')
        .map((l) => '> ' + l)
        .join('\n')
    }
    default:
      return mdChildren(el)
  }
}

export function toMarkdown(root: Node): string {
  return mdChildren(root).replace(/\n{3,}/g, '\n\n').trim()
}

// --- editing commands -----------------------------------------------------

function exec(cmd: string, value?: string) {
  document.execCommand(cmd, false, value)
}

function selectionIn(root: HTMLElement): Selection | null {
  const sel = window.getSelection()
  if (!sel || !sel.rangeCount || !root.contains(sel.anchorNode)) return null
  return sel
}

function closestEl(node: Node | null, selector: string): HTMLElement | null {
  const el = node?.nodeType === Node.ELEMENT_NODE ? (node as HTMLElement) : node?.parentElement ?? null
  return el?.closest(selector) ?? null
}

function toggleInlineCode(root: HTMLElement) {
  const sel = selectionIn(root)
  if (!sel) return
  const range = sel.getRangeAt(0)
  const existing = closestEl(range.commonAncestorContainer, 'code')
  if (existing && !existing.closest('pre')) {
    const text = document.createTextNode(existing.textContent ?? '')
    existing.replaceWith(text)
    sel.removeAllRanges()
    const r = document.createRange()
    r.selectNodeContents(text)
    sel.addRange(r)
    return
  }
  if (range.collapsed) {
    exec('insertText', '``')
    ;(sel as unknown as { modify(a: string, d: string, g: string): void }).modify('move', 'backward', 'character')
    return
  }
  try {
    const code = document.createElement('code')
    range.surroundContents(code)
    sel.removeAllRanges()
    const r = document.createRange()
    r.selectNodeContents(code)
    sel.addRange(r)
  } catch {
    exec('insertText', '`' + range.toString() + '`')
  }
}

function toggleBlock(root: HTMLElement, tag: 'blockquote' | 'pre') {
  const sel = selectionIn(root)
  if (!sel) return
  const inside = closestEl(sel.anchorNode, tag)
  exec('formatBlock', inside ? 'div' : tag)
}

function insertLink(root: HTMLElement) {
  const sel = selectionIn(root)
  if (!sel) return
  const url = window.prompt('Link address (https://…)')
  if (!url) return
  const href = /^[a-z]+:\/\//i.test(url) ? url : 'https://' + url
  if (sel.getRangeAt(0).collapsed) {
    const a = document.createElement('a')
    a.href = href
    a.textContent = href
    exec('insertHTML', a.outerHTML + ' ')
  } else {
    exec('createLink', href)
  }
}

// --- component ------------------------------------------------------------

export const RichEditor = forwardRef<RichEditorHandle, Props>(function RichEditor(
  { placeholder, toolbar, onInput, onSubmit, onFiles, onKeyDown, onBlur, onClick },
  ref,
) {
  const root = useRef<HTMLDivElement>(null)

  function normalizeEmpty() {
    const el = root.current
    if (!el) return
    const html = el.innerHTML.replace(/\s/g, '')
    if (html === '<br>' || html === '<div><br></div>' || html === '<p><br></p>') el.innerHTML = ''
  }

  useImperativeHandle(ref, () => ({
    focus: () => root.current?.focus(),
    clear: () => {
      if (root.current) root.current.innerHTML = ''
    },
    isEmpty: () => !root.current || toMarkdown(root.current) === '',
    getMarkdown: () => (root.current ? toMarkdown(root.current) : ''),
    textBeforeCaret: () => {
      const el = root.current
      const sel = el && selectionIn(el)
      if (!el || !sel) return ''
      const r = sel.getRangeAt(0).cloneRange()
      r.collapse(true)
      r.setStart(el, 0)
      return mdChildren(r.cloneContents())
    },
    replaceBeforeCaret: (count, text) => {
      const el = root.current
      const sel = el && selectionIn(el)
      if (!el || !sel) return
      el.focus()
      const s = sel as unknown as { modify(a: string, d: string, g: string): void }
      for (let i = 0; i < count; i++) s.modify('extend', 'backward', 'character')
      exec('insertText', text)
    },
  }))

  function keyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (onKeyDown?.(e)) return
    const mod = e.ctrlKey || e.metaKey
    if (mod && !e.altKey) {
      const k = e.key.toLowerCase()
      if (k === 'b') return void (e.preventDefault(), exec('bold'))
      if (k === 'i') return void (e.preventDefault(), exec('italic'))
      if (k === 'e') return void (e.preventDefault(), root.current && toggleInlineCode(root.current))
      if (k === 'k') return void (e.preventDefault(), root.current && insertLink(root.current))
      if (k === 'x' && e.shiftKey) return void (e.preventDefault(), exec('strikeThrough'))
      if (k === 'enter') return void (e.preventDefault(), onSubmit())
    }
    if (e.key === 'Enter') {
      if (e.shiftKey) {
        e.preventDefault()
        if (!document.execCommand('insertLineBreak')) exec('insertHTML', '<br>')
        return
      }
      // Inside a list, quote or code block Enter keeps writing; Ctrl+Enter sends.
      const sel = root.current && selectionIn(root.current)
      if (sel && closestEl(sel.anchorNode, 'li, blockquote, pre')) return
      e.preventDefault()
      onSubmit()
    }
  }

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      {toolbar && (
        <div className="flex flex-wrap items-center gap-0.5 px-1 pt-0.5" onMouseDown={(e) => e.preventDefault()}>
          <Tool title="Bold (Ctrl+B)" onClick={() => exec('bold')}>
            <Bold size={15} />
          </Tool>
          <Tool title="Italic (Ctrl+I)" onClick={() => exec('italic')}>
            <Italic size={15} />
          </Tool>
          <Tool title="Strikethrough (Ctrl+Shift+X)" onClick={() => exec('strikeThrough')}>
            <Strikethrough size={15} />
          </Tool>
          <Tool title="Code (Ctrl+E)" onClick={() => root.current && toggleInlineCode(root.current)}>
            <Code size={15} />
          </Tool>
          <Tool title="Link (Ctrl+K)" onClick={() => root.current && insertLink(root.current)}>
            <Link2 size={15} />
          </Tool>
          <span className="mx-1 h-4 w-px bg-edge" />
          <Tool title="Bulleted list" onClick={() => exec('insertUnorderedList')}>
            <List size={15} />
          </Tool>
          <Tool title="Numbered list" onClick={() => exec('insertOrderedList')}>
            <ListOrdered size={15} />
          </Tool>
          <Tool title="Quote" onClick={() => root.current && toggleBlock(root.current, 'blockquote')}>
            <Quote size={15} />
          </Tool>
        </div>
      )}
      <div
        ref={root}
        contentEditable
        suppressContentEditableWarning
        role="textbox"
        aria-multiline="true"
        aria-label={placeholder}
        data-placeholder={placeholder}
        className={cx(
          'md max-h-48 min-h-[2.5rem] flex-1 overflow-y-auto whitespace-pre-wrap break-words px-1 py-2 text-[0.925rem] text-ink focus:outline-none',
          'empty:before:pointer-events-none empty:before:text-ink-2 empty:before:content-[attr(data-placeholder)]',
        )}
        onInput={() => {
          normalizeEmpty()
          onInput()
        }}
        onKeyDown={keyDown}
        onBlur={onBlur}
        onClick={onClick}
        onKeyUp={onClick}
        onPaste={(e) => {
          const files = Array.from(e.clipboardData.files)
          if (files.length) {
            e.preventDefault()
            onFiles(files)
            return
          }
          // Plain text only, so pasted web pages don't bring their styling.
          const text = e.clipboardData.getData('text/plain')
          if (text) {
            e.preventDefault()
            exec('insertText', text)
          }
        }}
      />
    </div>
  )
})

function Tool({ title, onClick, children }: { title: string; onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" title={title} onClick={onClick} className="rounded-md p-1 text-ink-2 hover:bg-bg-3 hover:text-ink">
      {children}
    </button>
  )
}

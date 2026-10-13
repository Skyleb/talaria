import { marked } from 'marked'
import DOMPurify from 'dompurify'

// Chat-safe markdown: inline styles, code, quotes, lists, links. No raw HTML,
// no headings-as-huge-text abuse, no images (attachments handle media).
marked.use({
  gfm: true,
  breaks: true,
  renderer: {
    html: () => '',
    image: () => '',
    heading({ tokens }) {
      return `<p><strong>${this.parser.parseInline(tokens)}</strong></p>`
    },
    link({ href, tokens }) {
      const label = this.parser.parseInline(tokens)
      try {
        const u = new URL(href, window.location.origin)
        if (u.protocol !== 'http:' && u.protocol !== 'https:') return label
        return `<a href="${u.href}" target="_blank" rel="noreferrer noopener">${label}</a>`
      } catch {
        return label
      }
    },
  },
})

// renderMarkdown turns chat text into safe HTML. mentions maps lowercase
// usernames to display names; matching @tokens become highlighted chips.
export function renderMarkdown(src: string, mentions?: Record<string, string>): string {
  let html = marked.parse(src, { async: false }) as string
  if (mentions) {
    html = html.replace(/(^|[^\w@\/])@([a-z0-9_.]{2,32})/gi, (all, pre: string, name: string) => {
      const exact = mentions[name.toLowerCase()]
      const trimmed = name.replace(/\.+$/, '')
      const display = exact ?? mentions[trimmed.toLowerCase()]
      if (!display) return all
      const trail = exact ? '' : name.slice(trimmed.length)
      return `${pre}<span class="mention">@${display}</span>${trail}`
    })
  }
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: [
      'p', 'br', 'strong', 'em', 'del', 'code', 'pre', 'blockquote',
      'ul', 'ol', 'li', 'a', 'span',
    ],
    ALLOWED_ATTR: ['href', 'target', 'rel', 'class'],
  })
}

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

export function renderMarkdown(src: string): string {
  const html = marked.parse(src, { async: false }) as string
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: [
      'p', 'br', 'strong', 'em', 'del', 'code', 'pre', 'blockquote',
      'ul', 'ol', 'li', 'a',
    ],
    ALLOWED_ATTR: ['href', 'target', 'rel'],
  })
}

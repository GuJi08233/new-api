/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import DOMPurify from 'dompurify';

// 富文本文档（公告 / 关于 / 首页 / 页脚等）允许的扩展标签与属性。
// 与 web/default 的 html-content.tsx 保持一致：允许排版与媒体，禁止脚本与外部资源劫持。
const RICH_CONTENT_OPTIONS = {
  ADD_ATTR: [
    'allowfullscreen',
    'autoplay',
    'class',
    'controls',
    'default',
    'id',
    'kind',
    'label',
    'loading',
    'loop',
    'muted',
    'playsinline',
    'poster',
    'preload',
    'referrerpolicy',
    'rel',
    'srclang',
    'style',
    'target',
  ],
  ADD_TAGS: ['audio', 'iframe', 'picture', 'source', 'style', 'track', 'video'],
  FORBID_ATTR: ['srcdoc'],
  FORBID_TAGS: ['base', 'embed', 'link', 'meta', 'object', 'script'],
  FORCE_BODY: true,
};

const EXTERNAL_URL_SCHEMES = new Set(['http:', 'https:', 'mailto:', 'tel:']);

/**
 * 净化不受信任的 HTML 片段（Markdown 渲染结果、公告正文等）。
 * @param {string} html 原始 HTML
 * @returns {string} 可安全交给 dangerouslySetInnerHTML 的 HTML
 */
export function sanitizeHTML(html) {
  if (!html || typeof html !== 'string') return '';
  return DOMPurify.sanitize(html);
}

/**
 * 净化管理员配置的富文本文档，保留排版与媒体标签，但移除脚本、srcdoc 等注入面。
 * 同时为 target="_blank" 链接补 rel，并给 iframe 加 sandbox。
 * @param {string} html 原始 HTML
 * @returns {string} 净化后的 HTML
 */
export function sanitizeRichHTML(html) {
  if (!html || typeof html !== 'string') return '';

  const clean = DOMPurify.sanitize(html, RICH_CONTENT_OPTIONS);
  if (typeof document === 'undefined') return clean;

  const template = document.createElement('template');
  template.innerHTML = clean;

  template.content.querySelectorAll('a[target="_blank"]').forEach((link) => {
    const rel = new Set(
      link.getAttribute('rel')?.split(/\s+/).filter(Boolean) ?? [],
    );
    rel.add('noopener');
    rel.add('noreferrer');
    link.setAttribute('rel', [...rel].join(' '));
  });

  template.content.querySelectorAll('iframe').forEach((frame) => {
    frame.removeAttribute('srcdoc');
    frame.setAttribute(
      'sandbox',
      'allow-forms allow-popups allow-popups-to-escape-sandbox allow-presentation',
    );
    frame.setAttribute('referrerpolicy', 'no-referrer');
    if (!frame.hasAttribute('loading')) {
      frame.setAttribute('loading', 'lazy');
    }
  });

  return template.innerHTML;
}

/**
 * 净化注入到 document.head 的 CSS 文本。
 * DOMPurify 无法直接处理裸 CSS，这里让它以 style 标签的形式过一遍解析器，
 * 从而拦截通过 </style> 闭合标签逃逸出去的脚本。
 * @param {string} css 原始 CSS 文本
 * @returns {string} 净化后的 CSS 文本
 */
export function sanitizeCSS(css) {
  if (!css || typeof css !== 'string') return '';

  const wrapped = DOMPurify.sanitize(`<style>${css}</style>`, {
    ADD_TAGS: ['style'],
    FORCE_BODY: true,
  });
  const template = document.createElement('template');
  template.innerHTML = wrapped;

  return Array.from(template.content.querySelectorAll('style'))
    .map((style) => style.textContent || '')
    .join('\n');
}

/**
 * 校验一个 URL 是否可以安全地在新标签页打开。
 * 拒绝 javascript:、data: 等可执行脚本的协议。
 * @param {string} url 待校验的 URL
 * @returns {boolean}
 */
export function isSafeExternalUrl(url) {
  if (!url || typeof url !== 'string') return false;

  try {
    const parsed = new URL(url, window.location.origin);
    return EXTERNAL_URL_SCHEMES.has(parsed.protocol);
  } catch {
    return false;
  }
}

/**
 * 安全地在新标签页打开外部链接：校验协议白名单并阻断反向 tabnabbing。
 * @param {string} url 目标 URL
 * @returns {boolean} 是否成功打开
 */
export function openExternal(url) {
  if (!isSafeExternalUrl(url)) {
    console.error('[openExternal] blocked unsafe url:', url);
    return false;
  }

  window.open(url, '_blank', 'noopener,noreferrer');
  return true;
}

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

import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { isSafeExternalUrl, isStandaloneHttpUrl } from './sanitize';

describe('isStandaloneHttpUrl', () => {
  const originalWindow = globalThis.window;

  beforeEach(() => {
    globalThis.window = {
      location: { origin: 'https://example.test' },
    };
  });

  afterEach(() => {
    if (originalWindow === undefined) delete globalThis.window;
    else globalThis.window = originalWindow;
  });

  test.each([
    ['https://example.com/privacy'],
    ['  http://example.com/terms?v=1#top  '],
  ])('识别独立的 HTTP(S) 外部链接: %s', (input) => {
    expect(isStandaloneHttpUrl(input)).toBe(true);
  });

  test.each([
    [''],
    ['   \n  '],
    ['# 隐私政策\n\n我们重视您的隐私。'],
    ['欢迎使用本服务，请仔细阅读以下条款。'],
    ['<h1>用户协议</h1><p>第一条 总则</p>'],
    ['/privacy-policy'],
    ['https://example.test/privacy\n附加说明'],
    ['https://example.test/privacy 点击查看详情'],
    ['javascript:alert(1)'],
    ['data:text/html,<script>alert(1)</script>'],
    ['mailto:legal@example.com'],
    [null],
    [undefined],
  ])('不将正文、相对路径或非 HTTP(S) 内容误判为外部链接: %p', (input) => {
    expect(isStandaloneHttpUrl(input)).toBe(false);
  });
});

describe('isSafeExternalUrl', () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    if (originalWindow === undefined) delete globalThis.window;
    else globalThis.window = originalWindow;
  });

  test('在无 window 环境与浏览器环境下均放行安全协议并拦截伪协议', () => {
    delete globalThis.window;
    expect(isSafeExternalUrl('https://example.com/pay')).toBe(true);
    expect(isSafeExternalUrl('javascript:alert(1)')).toBe(false);
    expect(isSafeExternalUrl('   ')).toBe(false);

    globalThis.window = { location: { origin: 'https://example.test' } };
    expect(isSafeExternalUrl('/pay/callback')).toBe(true);
    expect(isSafeExternalUrl('data:text/html,hi')).toBe(false);
  });
});

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

// 24 点算式的本地求值，规则与服务端 pkg/twentyfour 一致：加减乘除与括号(× ÷ − 与全角括号也认)，用分数精确计算。
// 提交前先在本地算一遍，写错时当场提示，不浪费一次请求。

const gcd = (a, b) => (b === 0 ? Math.abs(a) : gcd(b, a % b));

function fraction(n, d = 1) {
  if (d < 0) {
    n = -n;
    d = -d;
  }
  const g = gcd(n, d) || 1;
  return { n: n / g, d: d / g };
}

const ADD = new Set(['+', '-', '−']);
const MUL = new Set(['*', '×', 'x', 'X']);
const DIV = new Set(['/', '÷']);

// evaluateExpression 返回 { value, numbers }：value 是分数 {n, d}，numbers 是依次用到的数；写法不对时返回 null。
export function evaluateExpression(text) {
  const input = Array.from(text || '');
  let pos = 0;
  const numbers = [];
  const peek = () => {
    while (input[pos] === ' ' || input[pos] === '\t') pos++;
    return input[pos];
  };
  const expression = () => {
    let left = term();
    while (left && ADD.has(peek())) {
      const op = input[pos++];
      const right = term();
      if (!right) return null;
      left =
        op === '+'
          ? fraction(left.n * right.d + right.n * left.d, left.d * right.d)
          : fraction(left.n * right.d - right.n * left.d, left.d * right.d);
    }
    return left;
  };
  const term = () => {
    let left = factor();
    while (left && (MUL.has(peek()) || DIV.has(peek()))) {
      const op = input[pos++];
      const right = factor();
      if (!right) return null;
      if (MUL.has(op)) left = fraction(left.n * right.n, left.d * right.d);
      else if (right.n === 0) return null;
      else left = fraction(left.n * right.d, left.d * right.n);
    }
    return left;
  };
  const factor = () => {
    const c = peek();
    if (c === '(' || c === '（') {
      pos++;
      const value = expression();
      const close = peek();
      if (!value || (close !== ')' && close !== '）')) return null;
      pos++;
      return value;
    }
    if (!(c >= '0' && c <= '9')) return null;
    let n = 0;
    let digits = 0;
    while (input[pos] >= '0' && input[pos] <= '9') {
      n = n * 10 + Number(input[pos++]);
      if (++digits > 2) return null;
    }
    if (numbers.length >= 4) return null;
    numbers.push(n);
    return fraction(n);
  };
  const value = expression();
  if (!value || peek() !== undefined) return null;
  return { value, numbers };
}

// checkAnswer 检查算式能不能过这一题：syntax 是写法不对，numbers 是没有正好用上这四张牌，value 是算出来不是 24。
export function checkAnswer(text, hand) {
  if (!text || text.length > 64) return { ok: false, reason: 'syntax' };
  const parsed = evaluateExpression(text);
  if (!parsed) return { ok: false, reason: 'syntax' };
  const used = [...parsed.numbers].sort((a, b) => a - b).join(',');
  const want = [...hand].sort((a, b) => a - b).join(',');
  if (used !== want) return { ok: false, reason: 'numbers', parsed };
  if (parsed.value.n !== 24 || parsed.value.d !== 1)
    return { ok: false, reason: 'value', parsed };
  return { ok: true, parsed };
}

// formatFraction 把分数写成 7 或 7/3。
export const formatFraction = ({ n, d }) => (d === 1 ? `${n}` : `${n}/${d}`);

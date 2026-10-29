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

// 猜数字的提示：guess 相对 secret 位置和数字都对的个数 a，数字对、位置不对的个数 b。与服务端 pkg/bulls.Score 一致。
export function scoreGuess(secret, guess) {
  let a = 0;
  let b = 0;
  for (let i = 0; i < guess.length; i++) {
    const at = secret.indexOf(guess[i]);
    if (at === i) a++;
    else if (at >= 0) b++;
  }
  return { a, b };
}

// consistentWith 表示 guess 若是答案，之前每一次猜测得到的提示都对得上。玩家据此知道这一猜是否浪费了已知线索。
export const consistentWith = (guess, history) =>
  history.every((item) => {
    const { a, b } = scoreGuess(guess, item.guess);
    return a === item.a && b === item.b;
  });

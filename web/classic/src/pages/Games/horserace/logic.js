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

// 赛马的比赛演出。名次由服务端抽好，这里只负责编一段好看的过程：每匹马按名次定好冲线时刻，途中的快慢起伏随机，
// 但保证每匹马只往前跑、在自己的冲线时刻正好到终点，所以最后的名次一定和服务端一致。

// 第一名冲线的时刻与之后每个名次落后的时间(毫秒)。
export const WINNER_MS = 6200;
export const GAP_MS = 260;

// 一个简单的可复现随机数(mulberry32)，同一场比赛的过程每次重画都一样。
export function randomOf(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

// 为 order(名次，第一名在前)编排每匹马的跑法：冲线时刻，以及两段起伏(幅度、频率、相位)。后来居上的马前半程压着跑。
export function planRace(order, seed) {
  const random = randomOf(seed);
  const plans = [];
  order.forEach((horse, rank) => {
    const late = rank === 0 && random() < 0.6;
    plans[horse] = {
      finish: WINNER_MS + rank * GAP_MS + random() * 80,
      waves: [
        {
          amp: late ? -0.14 : (random() - 0.5) * 0.22,
          freq: 0.5,
          phase: 0,
        },
        {
          amp: (random() - 0.5) * 0.08,
          freq: 1 + random() * 2,
          phase: random(),
        },
      ],
    };
  });
  return plans;
}

// 第 elapsed 毫秒时这匹马跑了全程的几成(0..1)。u(1 - u) 包住的起伏在起点和终点都是 0；幅度不大，跑法始终向前。
export function progressAt(plan, elapsed) {
  const u = Math.min(1, Math.max(0, elapsed / plan.finish));
  let wobble = 0;
  for (const wave of plan.waves) {
    wobble +=
      wave.amp *
      Math.sin(Math.PI * 2 * (wave.freq * u + wave.phase)) *
      (wave.freq === 0.5 ? 1 : 0.5);
  }
  return Math.min(1, Math.max(0, u + wobble * u * (1 - u) * 2));
}

// 第 elapsed 毫秒时跑在最前面的马。
export function leaderAt(plans, elapsed) {
  let leader = 0;
  let best = -1;
  plans.forEach((plan, horse) => {
    const p = progressAt(plan, elapsed);
    if (p > best) {
      best = p;
      leader = horse;
    }
  });
  return leader;
}

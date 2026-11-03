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

import { afterEach, beforeEach, expect, test } from 'bun:test';
import { createControls } from './controls';
import { BTN_ATTACK, BTN_SKILL, BTN_SWAP, pack } from './engine';

let previousWindow;
let canvas;
let controls;
const fight = { p: { x: 0, y: 0 } };
const view = { camX: 0, camY: 0, zoom: 1 };

beforeEach(() => {
  previousWindow = globalThis.window;
  globalThis.window = new EventTarget();
  canvas = new EventTarget();
  canvas.getBoundingClientRect = () => ({ left: 0, top: 0 });
  controls = createControls({ canvas, onPause: () => {} });
});

afterEach(() => {
  controls.dispose();
  if (previousWindow === undefined) delete globalThis.window;
  else globalThis.window = previousWindow;
});

test('暂停清除尚未取样的按键，继续后不会迟放法术或换法宝', () => {
  controls.press(BTN_SKILL, 1);
  controls.press(BTN_SWAP, 2);
  controls.setStick(16);
  controls.reset();
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
});

test('同一攻击键的第二根手指松开不影响仍按住的手指', () => {
  controls.press(BTN_ATTACK, 1);
  controls.press(BTN_ATTACK, 2);
  controls.sample(fight, view);
  controls.release(BTN_ATTACK, 2);
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, BTN_ATTACK));
  controls.release(BTN_ATTACK, 1);
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
});

test('取消未取样的触屏操作不会留下动作，快速正常点按仍执行一帧', () => {
  controls.press(BTN_SKILL, 1);
  controls.release(BTN_SKILL, 1, true);
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
  controls.press(BTN_ATTACK, 2);
  controls.release(BTN_ATTACK, 2);
  controls.release(BTN_ATTACK, 2, true); // pointerup 后的 lostpointercapture 不能取消已完成的点按。
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, BTN_ATTACK));
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
});

test('不同触屏键的指针互不释放，未知指针也不能松开已按下的键', () => {
  controls.press(BTN_ATTACK, 1);
  controls.press(BTN_SKILL, 2);
  controls.sample(fight, view);
  controls.release(BTN_ATTACK, 2);
  controls.release(BTN_ATTACK, 3);
  expect(controls.sample(fight, view)).toBe(
    pack(-1, -1, BTN_ATTACK | BTN_SKILL),
  );
  controls.release(BTN_SKILL, 2, true);
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, BTN_ATTACK));
});

test('触屏输入恢复自动瞄准，窗口失焦清除所有待处理输入', () => {
  const move = new Event('mousemove');
  Object.assign(move, { clientX: 100, clientY: 0 });
  canvas.dispatchEvent(move);
  expect(controls.sample(fight, view)).toBe(pack(-1, 0, 0));
  controls.press(BTN_ATTACK, 1);
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, BTN_ATTACK));
  controls.press(BTN_SKILL, 2);
  controls.setStick(16);
  window.dispatchEvent(new Event('blur'));
  expect(controls.sample(fight, view)).toBe(pack(-1, -1, 0));
});

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

// 斗法的操作：每帧取样一次，压成规则要的一个整数(engine.js 的 pack)。
// 键盘：WASD/方向键移动，J 攻击(自动瞄准)，K 法术，空格/Shift/L 身法，Q 换法宝，Esc/P 暂停。
// 鼠标：在画面上移动就按鼠标瞄准，左键攻击，右键法术；按 J 攻击时改回自动瞄准。
// 触屏：左边摇杆给方向(setStick)，右边按钮按下松开(press/release)，触屏一律自动瞄准。
// 很快的一下点按可能落在两帧之间，所以按下时先记一笔，取样时至少让规则看到一帧按住。
import {
  BTN_ATTACK,
  BTN_DODGE,
  BTN_SKILL,
  BTN_SWAP,
  angleOf,
  pack,
} from './engine';

const KEY_BUTTONS = {
  KeyJ: BTN_ATTACK,
  KeyK: BTN_SKILL,
  Space: BTN_DODGE,
  ShiftLeft: BTN_DODGE,
  ShiftRight: BTN_DODGE,
  KeyL: BTN_DODGE,
  KeyQ: BTN_SWAP,
};
const MOVE_KEYS = {
  KeyW: [0, -1],
  ArrowUp: [0, -1],
  KeyS: [0, 1],
  ArrowDown: [0, 1],
  KeyA: [-1, 0],
  ArrowLeft: [-1, 0],
  KeyD: [1, 0],
  ArrowRight: [1, 0],
};

export function createControls({ canvas, onPause }) {
  const keys = new Set();
  const pointers = new Map();
  let held = 0;
  let latched = 0;
  let stick = -1;
  let mouse = null;
  let mouseAim = false;
  let mouseButtons = 0;

  const typing = (e) => {
    const tag = e.target?.tagName;
    return tag === 'INPUT' || tag === 'TEXTAREA' || e.target?.isContentEditable;
  };

  const onKeyDown = (e) => {
    if (typing(e) || e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.code === 'Escape' || e.code === 'KeyP') {
      if (!e.repeat) onPause();
      e.preventDefault();
      return;
    }
    const bit = KEY_BUTTONS[e.code];
    if (!bit && !MOVE_KEYS[e.code]) return;
    e.preventDefault();
    if (keys.has(e.code)) return;
    keys.add(e.code);
    if (bit) latched |= bit;
    if (e.code === 'KeyJ') mouseAim = false;
  };
  const onKeyUp = (e) => {
    keys.delete(e.code);
  };
  const onBlur = () => {
    keys.clear();
    pointers.clear();
    mouseButtons = 0;
    held = 0;
    latched = 0;
    stick = -1;
    mouse = null;
    mouseAim = false;
  };
  const point = (e) => {
    const box = canvas.getBoundingClientRect();
    mouse = { x: e.clientX - box.left, y: e.clientY - box.top };
  };
  const onMouseMove = (e) => {
    point(e);
    mouseAim = true;
  };
  const onMouseDown = (e) => {
    point(e);
    mouseAim = true;
    if (e.button === 0) {
      mouseButtons |= BTN_ATTACK;
      latched |= BTN_ATTACK;
    } else if (e.button === 2) {
      mouseButtons |= BTN_SKILL;
      latched |= BTN_SKILL;
    }
  };
  const onMouseUp = (e) => {
    if (e.button === 0) mouseButtons &= ~BTN_ATTACK;
    else if (e.button === 2) mouseButtons &= ~BTN_SKILL;
  };
  const onContextMenu = (e) => e.preventDefault();

  window.addEventListener('keydown', onKeyDown);
  window.addEventListener('keyup', onKeyUp);
  window.addEventListener('blur', onBlur);
  window.addEventListener('mouseup', onMouseUp);
  canvas.addEventListener('mousemove', onMouseMove);
  canvas.addEventListener('mousedown', onMouseDown);
  canvas.addEventListener('contextmenu', onContextMenu);

  return {
    // sample 取这一帧的输入；view 是画面的镜头(把鼠标位置换成房间里的坐标)。
    sample(f, view) {
      let dx = 0;
      let dy = 0;
      for (const code of keys) {
        const dir = MOVE_KEYS[code];
        if (dir) {
          dx += dir[0];
          dy += dir[1];
        }
      }
      let move = stick;
      if (dx !== 0 || dy !== 0) move = angleOf(dx * 1000, dy * 1000);
      let buttons = held | latched | mouseButtons;
      for (const code of keys) buttons |= KEY_BUTTONS[code] || 0;
      latched = 0;
      let aim = -1;
      if (mouseAim && mouse && view) {
        const wx = (view.camX + mouse.x / view.zoom) * 100;
        const wy = (view.camY + mouse.y / view.zoom) * 100;
        if (Math.abs(wx - f.p.x) + Math.abs(wy - f.p.y) > 200)
          aim = angleOf(Math.round(wx - f.p.x), Math.round(wy - f.p.y));
      }
      return pack(move, aim, buttons);
    },
    setStick(angle) {
      stick = angle;
      mouseAim = false;
    },
    press(bit, pointerId = bit) {
      if (pointers.has(pointerId)) return;
      pointers.set(pointerId, bit);
      held |= bit;
      latched |= bit;
      mouseAim = false;
    },
    release(bit, pointerId = bit, cancelled = false) {
      if (pointers.get(pointerId) !== bit) return;
      pointers.delete(pointerId);
      held = 0;
      for (const pressed of pointers.values()) held |= pressed;
      if (cancelled && !(held & bit)) latched &= ~bit;
    },
    reset: onBlur,
    dispose() {
      window.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('keyup', onKeyUp);
      window.removeEventListener('blur', onBlur);
      window.removeEventListener('mouseup', onMouseUp);
      canvas.removeEventListener('mousemove', onMouseMove);
      canvas.removeEventListener('mousedown', onMouseDown);
      canvas.removeEventListener('contextmenu', onContextMenu);
    },
  };
}

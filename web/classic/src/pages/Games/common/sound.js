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

import { useCallback, useSyncExternalStore } from 'react';

// 游戏中心共用的音效：用 Web Audio 现场合成几种短音，不下载素材。开关保存在当前浏览器，所有游戏共用(象棋另有自己的
// 落子录音与开关)。浏览器要求用户操作之后才能出声：第一次在点击、按键里调用 playSound 时创建音频上下文，之前的调用
// 直接忽略；不支持或被拒绝时游戏照常运行。

const SOUND_KEY = 'game_sound_enabled';

// 每种音效是几段音：频率(Hz，slide 是结束时的频率)、开始时刻与时长(秒)、波形和音量。噪声(noise)用来做爆炸、刮擦。
const TONES = {
  click: [{ freq: 660, dur: 0.04, type: 'square', gain: 0.12 }],
  move: [{ freq: 420, dur: 0.05, type: 'triangle', gain: 0.22 }],
  rotate: [{ freq: 540, slide: 720, dur: 0.06, type: 'triangle', gain: 0.18 }],
  place: [
    { freq: 220, slide: 140, dur: 0.09, type: 'sine', gain: 0.45 },
    { noise: true, dur: 0.03, gain: 0.08 },
  ],
  drop: [{ freq: 300, slide: 90, dur: 0.12, type: 'sine', gain: 0.4 }],
  merge: [{ freq: 520, slide: 780, dur: 0.08, type: 'sine', gain: 0.28 }],
  clear: [
    { freq: 523, dur: 0.08, type: 'triangle', gain: 0.3 },
    { freq: 784, at: 0.07, dur: 0.12, type: 'triangle', gain: 0.3 },
  ],
  combo: [
    { freq: 523, dur: 0.07, type: 'triangle', gain: 0.3 },
    { freq: 659, at: 0.06, dur: 0.07, type: 'triangle', gain: 0.3 },
    { freq: 784, at: 0.12, dur: 0.07, type: 'triangle', gain: 0.3 },
    { freq: 1047, at: 0.18, dur: 0.16, type: 'triangle', gain: 0.32 },
  ],
  eat: [{ freq: 600, slide: 900, dur: 0.07, type: 'square', gain: 0.1 }],
  flap: [{ freq: 380, slide: 520, dur: 0.06, type: 'sine', gain: 0.18 }],
  score: [
    { freq: 880, dur: 0.06, type: 'square', gain: 0.09 },
    { freq: 1175, at: 0.06, dur: 0.09, type: 'square', gain: 0.09 },
  ],
  hit: [{ freq: 330, dur: 0.04, type: 'square', gain: 0.12 }],
  break: [
    { freq: 760, slide: 420, dur: 0.07, type: 'triangle', gain: 0.22 },
    { noise: true, dur: 0.05, gain: 0.06 },
  ],
  flip: [{ noise: true, dur: 0.05, gain: 0.12, filter: 2400 }],
  deal: [{ noise: true, dur: 0.07, gain: 0.14, filter: 1800 }],
  chip: [
    { freq: 1900, dur: 0.03, type: 'square', gain: 0.06 },
    { freq: 2500, at: 0.03, dur: 0.04, type: 'square', gain: 0.05 },
  ],
  tick: [{ freq: 1400, dur: 0.018, type: 'square', gain: 0.06 }],
  peg: [{ freq: 1250, slide: 1100, dur: 0.03, type: 'sine', gain: 0.1 }],
  reveal: [{ freq: 700, slide: 1000, dur: 0.07, type: 'sine', gain: 0.2 }],
  scratch: [{ noise: true, dur: 0.06, gain: 0.05, filter: 3200 }],
  coin: [
    { freq: 988, dur: 0.07, type: 'square', gain: 0.1 },
    { freq: 1319, at: 0.07, dur: 0.2, type: 'square', gain: 0.1 },
  ],
  win: [
    { freq: 523, dur: 0.1, type: 'triangle', gain: 0.32 },
    { freq: 659, at: 0.1, dur: 0.1, type: 'triangle', gain: 0.32 },
    { freq: 784, at: 0.2, dur: 0.1, type: 'triangle', gain: 0.32 },
    { freq: 1047, at: 0.3, dur: 0.3, type: 'triangle', gain: 0.34 },
  ],
  bigWin: [
    { freq: 523, dur: 0.09, type: 'square', gain: 0.12 },
    { freq: 659, at: 0.09, dur: 0.09, type: 'square', gain: 0.12 },
    { freq: 784, at: 0.18, dur: 0.09, type: 'square', gain: 0.12 },
    { freq: 1047, at: 0.27, dur: 0.09, type: 'square', gain: 0.12 },
    { freq: 784, at: 0.36, dur: 0.09, type: 'square', gain: 0.12 },
    { freq: 1047, at: 0.45, dur: 0.4, type: 'square', gain: 0.13 },
    { freq: 523, at: 0.45, dur: 0.4, type: 'triangle', gain: 0.25 },
  ],
  lose: [
    { freq: 392, dur: 0.14, type: 'triangle', gain: 0.28 },
    { freq: 330, at: 0.13, dur: 0.14, type: 'triangle', gain: 0.28 },
    { freq: 262, at: 0.26, dur: 0.3, type: 'triangle', gain: 0.28 },
  ],
  error: [{ freq: 160, dur: 0.12, type: 'square', gain: 0.1 }],
  bomb: [
    { noise: true, dur: 0.45, gain: 0.4, filter: 900 },
    { freq: 120, slide: 40, dur: 0.4, type: 'sine', gain: 0.5 },
  ],
  start: [
    { freq: 440, dur: 0.07, type: 'triangle', gain: 0.25 },
    { freq: 660, at: 0.07, dur: 0.12, type: 'triangle', gain: 0.25 },
  ],
  levelUp: [
    { freq: 659, dur: 0.07, type: 'square', gain: 0.1 },
    { freq: 880, at: 0.07, dur: 0.07, type: 'square', gain: 0.1 },
    { freq: 1319, at: 0.14, dur: 0.18, type: 'square', gain: 0.1 },
  ],
};

// 同一种音效这么短的间隔内只响一次：一帧里碰了好几块砖、一次落了好几颗球，不会叠成一声刺耳的爆音。
const MIN_GAP_SECONDS = 0.035;

let context = null;
let master = null;
let noiseBuffer = null;
const lastPlayed = new Map();
const listeners = new Set();

function readEnabled() {
  try {
    return localStorage.getItem(SOUND_KEY) !== 'false';
  } catch {
    return true;
  }
}

let enabled = readEnabled();

function subscribe(listener) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function isSoundEnabled() {
  return enabled;
}

export function setSoundEnabled(next) {
  enabled = next;
  try {
    localStorage.setItem(SOUND_KEY, String(next));
  } catch {
    // 隐私模式或存储空间不足时只影响本页。
  }
  listeners.forEach((listener) => listener());
  if (next) playSound('click');
}

// 音效开关的状态与切换，所有游戏页共用一个值。
export function useGameSound() {
  const on = useSyncExternalStore(subscribe, isSoundEnabled);
  const toggle = useCallback(() => setSoundEnabled(!enabled), []);
  return [on, toggle];
}

function audio() {
  if (context) {
    if (context.state === 'suspended') context.resume().catch(() => {});
    return context;
  }
  // 页面还没有收到过点击或按键时浏览器不让出声，先不创建，免得控制台报警告。
  if (navigator.userActivation && !navigator.userActivation.hasBeenActive) {
    return null;
  }
  const Context = window.AudioContext || window.webkitAudioContext;
  if (!Context) return null;
  context = new Context();
  master = context.createGain();
  master.gain.value = 0.5;
  master.connect(context.destination);
  const length = Math.floor(context.sampleRate * 0.5);
  noiseBuffer = context.createBuffer(1, length, context.sampleRate);
  const data = noiseBuffer.getChannelData(0);
  for (let i = 0; i < length; i++) data[i] = Math.random() * 2 - 1;
  return context;
}

function playTone(ctx, tone, start) {
  const begin = start + (tone.at || 0);
  const end = begin + tone.dur;
  const gain = ctx.createGain();
  gain.gain.setValueAtTime(0.0001, begin);
  gain.gain.exponentialRampToValueAtTime(tone.gain, begin + 0.008);
  gain.gain.exponentialRampToValueAtTime(0.0001, end);
  let source;
  if (tone.noise) {
    source = ctx.createBufferSource();
    source.buffer = noiseBuffer;
    if (tone.filter) {
      const filter = ctx.createBiquadFilter();
      filter.type = 'bandpass';
      filter.frequency.value = tone.filter;
      source.connect(filter);
      filter.connect(gain);
    } else {
      source.connect(gain);
    }
  } else {
    source = ctx.createOscillator();
    source.type = tone.type || 'sine';
    source.frequency.setValueAtTime(tone.freq, begin);
    if (tone.slide) {
      source.frequency.exponentialRampToValueAtTime(tone.slide, end);
    }
    source.connect(gain);
  }
  gain.connect(master);
  source.start(begin);
  source.stop(end + 0.02);
  source.onended = () => {
    source.disconnect();
    gain.disconnect();
  };
}

// 播放一种音效。pitch 把整段音按倍数升降调(连击越多越高)。关闭音效或浏览器还不允许出声时什么也不做。
export function playSound(name, { pitch = 1 } = {}) {
  if (!enabled) return;
  const tones = TONES[name];
  if (!tones) return;
  try {
    const ctx = audio();
    if (!ctx || ctx.state === 'closed') return;
    const now = ctx.currentTime;
    if (now - (lastPlayed.get(name) ?? -1) < MIN_GAP_SECONDS) return;
    lastPlayed.set(name, now);
    for (const tone of tones) {
      playTone(
        ctx,
        pitch === 1
          ? tone
          : {
              ...tone,
              freq: tone.freq && tone.freq * pitch,
              slide: tone.slide && tone.slide * pitch,
            },
        now + 0.005,
      );
    }
  } catch {
    // 音频出错不影响游戏。
  }
}

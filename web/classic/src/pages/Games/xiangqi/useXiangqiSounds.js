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

import { useCallback, useEffect, useRef, useState } from 'react';
import { xiangqiSoundTransition } from './soundEvents';

const SOUND_KEY = 'xiangqi_sound_enabled';

class XiangqiSoundPlayer {
  constructor() {
    this.context = null;
    this.buffers = null;
    this.resuming = null;
    this.sources = new Set();
    this.revision = 0;
    this.tail = 0;
    this.loading = new AbortController();
  }

  // 必须由用户手势调用。轮询与播放都不会自动重试浏览器拒绝的 resume。
  unlock() {
    try {
      const Context = window.AudioContext || window.webkitAudioContext;
      if (!Context) return;
      if (!this.context) {
        this.context = new Context();
        this.gain = this.context.createGain();
        this.gain.gain.value = 0.55;
        this.gain.connect(this.context.destination);
      }
      const context = this.context;
      if (context.state === 'suspended' && !this.resuming) {
        this.resuming = context
          .resume()
          .catch(() => {})
          .finally(() => {
            this.resuming = null;
          });
      }
      if (!this.buffers) {
        this.buffers = Promise.all(
          ['move', 'capture'].map(async (name) => {
            const response = await fetch(`/sounds/xiangqi/${name}.wav`, {
              signal: this.loading.signal,
            });
            if (!response.ok) throw new Error('Unable to load xiangqi sound');
            const buffer = await context.decodeAudioData(
              await response.arrayBuffer(),
            );
            return [name, buffer];
          }),
        )
          .then(Object.fromEntries)
          .catch(() => {
            // 临时下载/解码失败后，允许下一次用户操作重新加载。
            this.buffers = null;
            return null;
          });
      }
    } catch {
      // 浏览器不支持或拒绝音频时，棋局照常运行。
    }
  }

  play(sounds) {
    const context = this.context;
    if (
      !sounds.length ||
      !context ||
      (!this.resuming && context.state !== 'running') ||
      !this.buffers
    ) {
      return;
    }
    const revision = this.revision;
    Promise.all([this.buffers, this.resuming])
      .then(([buffers]) => {
        if (
          !buffers ||
          revision !== this.revision ||
          context.state !== 'running'
        ) {
          return;
        }
        for (const name of sounds) {
          const source = context.createBufferSource();
          source.buffer = buffers[name];
          source.connect(this.gain);
          source.onended = () => {
            this.sources.delete(source);
            source.disconnect();
          };
          this.sources.add(source);
          const start = Math.max(context.currentTime, this.tail);
          source.start(start);
          this.tail = start + source.buffer.duration + 0.04;
        }
      })
      .catch(() => this.clear());
  }

  clear() {
    this.revision++;
    this.tail = 0;
    for (const source of this.sources) {
      try {
        source.stop();
      } catch {
        // 已经自然结束的节点无需再次停止。
      }
      source.disconnect();
    }
    this.sources.clear();
  }

  close() {
    this.clear();
    this.loading.abort();
    this.context?.close().catch(() => {});
  }
}

export function useXiangqiSounds(state, gameId) {
  const [soundEnabled, setSoundEnabled] = useState(() => {
    try {
      return localStorage.getItem(SOUND_KEY) !== 'false';
    } catch {
      return true;
    }
  });
  const enabled = useRef(soundEnabled);
  const cursor = useRef(null);
  const player = useRef(null);

  const unlockSound = useCallback(() => {
    if (!enabled.current) return;
    if (!player.current) player.current = new XiangqiSoundPlayer();
    player.current.unlock();
  }, []);

  const toggleSound = useCallback(() => {
    const next = !enabled.current;
    enabled.current = next;
    setSoundEnabled(next);
    try {
      localStorage.setItem(SOUND_KEY, String(next));
    } catch {
      // 隐私模式或存储空间不足不影响本次开关。
    }
    if (next) unlockSound();
    else player.current?.clear();
  }, [unlockSound]);

  useEffect(() => {
    const transition = xiangqiSoundTransition(cursor.current, state, gameId);
    cursor.current = transition.cursor;
    if (transition.reset) player.current?.clear();
    else if (enabled.current) player.current?.play(transition.sounds);
  }, [gameId, state]);

  useEffect(
    () => () => {
      player.current?.close();
      player.current = null;
      cursor.current = null;
    },
    [],
  );

  return { soundEnabled, toggleSound, unlockSound };
}

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

import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Checkbox, Select } from '@douyinfe/semi-ui';
import { Repeat, Square } from 'lucide-react';
import { useTranslation } from 'react-i18next';

// 下注游戏的自动连玩：按当前的下注设置连续玩几局，每局照常请求服务端、照常播放开奖过程，揭晓之后隔一会儿再开下一局。
// 下注失败、余额不够、中了大奖(可选)、切到别的页面或玩家点停止时立即停下；只是替玩家按按钮，不改变任何规则和返还率。

const ROUND_OPTIONS = [10, 25, 50, 100];
// 两局之间停这么久，看得清上一局的结果。
const AUTO_GAP_MS = 700;

export function useAutoPlay() {
  const [state, setState] = useState(null);
  const stateRef = useRef(null);
  const timerRef = useRef(0);
  const playRef = useRef(null);

  const update = useCallback((next) => {
    stateRef.current = next;
    setState(next);
  }, []);

  const stop = useCallback(() => {
    clearTimeout(timerRef.current);
    update(null);
  }, [update]);

  // 每次渲染把最新的开局函数交给自动连玩，下一局用的是玩家当时的下注设置。
  const bind = (play) => {
    playRef.current = play;
  };

  const begin = useCallback(
    (rounds, stopOnBigWin) => {
      update({ left: rounds, total: rounds, stopOnBigWin });
      playRef.current?.();
    },
    [update],
  );

  // 一局揭晓之后调用。ok 为假(下注失败)或 bigWin 且设了大奖停止时停下，否则还有局数就隔一会儿开下一局。
  const after = useCallback(
    (ok, { bigWin = false } = {}) => {
      const current = stateRef.current;
      if (!current) return;
      const left = current.left - 1;
      if (!ok || left <= 0 || (bigWin && current.stopOnBigWin)) {
        stop();
        return;
      }
      update({ ...current, left });
      clearTimeout(timerRef.current);
      timerRef.current = setTimeout(() => playRef.current?.(), AUTO_GAP_MS);
    },
    [stop, update],
  );

  // 开奖过程比请求长的游戏(Plinko 的球还在落)先按请求结果接着连玩，等揭晓出大奖时再调用它：设了大奖停止就停下。
  const bigWin = useCallback(() => {
    if (stateRef.current?.stopOnBigWin) stop();
  }, [stop]);

  // 切到别的标签页或窗口时停下，免得在玩家看不到的时候一直花游戏币。
  useEffect(() => {
    if (!state) return undefined;
    const onHidden = () => {
      if (document.hidden) stop();
    };
    document.addEventListener('visibilitychange', onHidden);
    return () => document.removeEventListener('visibilitychange', onHidden);
  }, [state, stop]);

  useEffect(() => () => clearTimeout(timerRef.current), []);

  return {
    active: !!state,
    left: state?.left || 0,
    bind,
    begin,
    stop,
    after,
    bigWin,
  };
}

// 自动连玩的按钮：没在连玩时选局数与"中大奖停止"再开始，连玩中显示还剩几局和停止按钮。disabled 为真时(余额不足、
// 正在开奖)不能开始。
export const AutoPlayControls = ({ auto, disabled }) => {
  const { t } = useTranslation();
  const [rounds, setRounds] = useState(ROUND_OPTIONS[0]);
  const [stopOnBigWin, setStopOnBigWin] = useState(true);
  if (auto.active) {
    return (
      <div className='game-auto'>
        <Button type='danger' icon={<Square size={14} />} onClick={auto.stop}>
          {t('停止自动（还剩 {{left}} 局）', { left: auto.left })}
        </Button>
      </div>
    );
  }
  return (
    <div className='game-auto'>
      <Select
        size='small'
        value={rounds}
        onChange={setRounds}
        aria-label={t('自动局数')}
        optionList={ROUND_OPTIONS.map((value) => ({
          value,
          label: t('{{count}} 局', { count: value }),
        }))}
        style={{ width: 96 }}
      />
      <Button
        size='small'
        icon={<Repeat size={14} />}
        disabled={disabled}
        onClick={() => auto.begin(rounds, stopOnBigWin)}
      >
        {t('自动')}
      </Button>
      <Checkbox
        checked={stopOnBigWin}
        onChange={(event) => setStopOnBigWin(event.target.checked)}
      >
        {t('中大奖时停止')}
      </Checkbox>
    </div>
  );
};

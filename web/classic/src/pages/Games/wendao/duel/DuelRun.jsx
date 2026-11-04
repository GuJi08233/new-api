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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Modal } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { gamePostOnce, newRequestId } from '../../common/api';
import { playSound } from '../../common/sound';
import { eventSentence, useSectText } from '../../sect/parts';
import { FPS, encodeInputs } from '../doufa/engine';
import Fight from '../doufa/Fight';
import '../doufa/doufa.css';

const API = '/api/game/wendao/duel';

const formatTime = (ticks) => {
  const seconds = Math.floor(ticks / FPS);
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
};

// DuelRun 打一场斗法契约(入门试炼、宗门任务、悬赏)：接下契约拿到种子与开局，在浏览器里打完，把操作记录交给服务端重放核对，
// 然后看结果。接下就用掉一次挑战，页面中途离开只能放弃(种子只在接下的回应里给一次)。onDone 在回到原来的页面时调用，
// 参数是最后一次回应(契约、身份、宗门的新样子)，放弃或出错时可能为空。
export default function DuelRun({ kind, id, onDone }) {
  const { t } = useTranslation();
  const text = useSectText();
  const [phase, setPhase] = useState('starting');
  const [room, setRoom] = useState(null);
  const [answer, setAnswer] = useState(null);
  const [error, setError] = useState('');
  const started = useRef(false);
  const lastRun = useRef(null);
  const clearRequest = useRef(null);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    (async () => {
      const res = await gamePostOnce(`${API}/start`, { kind, id }, t);
      if (res.error || !res.data.duel) {
        setError(res.error || t('这一场已经开始过了，只能放弃'));
        setPhase('error');
        return;
      }
      setRoom(res.data.duel);
      setPhase('fight');
      playSound('start');
    })();
  }, [kind, id, t]);

  const submit = async (run) => {
    lastRun.current = run;
    if (!clearRequest.current) {
      clearRequest.current = {
        inputs: encodeInputs(run.inputs),
        battle_token: room.token,
        request_id: newRequestId(),
      };
    }
    setPhase('check');
    setError('');
    const res = await gamePostOnce(`${API}/clear`, clearRequest.current, t);
    if (res.error) {
      setError(res.error);
      setPhase('error');
      return;
    }
    // 升级前已结算的旧收据可能没有胜负快照，回到当前状态，避免把缺少结果当作失败。
    if (typeof res.data.won !== 'boolean') {
      onDone(res.data);
      return;
    }
    setAnswer(res.data);
    setPhase('result');
    playSound(res.data.won ? 'win' : 'lose');
  };

  const giveUp = async () => {
    if (document.fullscreenElement) document.exitFullscreen?.();
    Modal.confirm({
      title: t('放弃这一场'),
      content: t('放弃会用掉这次挑战，不能重来。'),
      onOk: async () => {
        const res = await gamePostOnce(
          `${API}/giveup`,
          { battle_token: room.token },
          t,
        );
        if (res.error) {
          setError(res.error);
          setPhase('error');
          return;
        }
        onDone(res.data || null);
      },
    });
  };

  if (phase === 'fight' && room) {
    return (
      <div className='df-panel'>
        <Fight
          key={room.seed}
          seed={room.seed}
          setup={room.setup}
          onEnd={submit}
          onExit={giveUp}
          exitLabel={t('放弃')}
        />
        <p className='df-muted'>
          {t('战斗中离开本页会放弃这一场，用掉的挑战不退。')}
        </p>
      </div>
    );
  }

  if (phase === 'result' && answer) {
    const lines = [];
    for (const ev of answer.events || []) {
      if (ev.k === 'res') {
        lines.push(`${text.resources[ev.s]} ${ev.n > 0 ? '+' : ''}${ev.n}`);
      } else {
        const line = eventSentence(ev, text, t);
        if (line) lines.push(line);
      }
    }
    return (
      <div className='df-panel'>
        <div className={`df-card df-result ${answer.won ? 'is-won' : ''}`}>
          <div className='df-title'>
            {answer.won ? t('凯旋') : t('败下阵来')}
          </div>
          <ul className='df-stats'>
            <li>{t('用时 {{time}}', { time: formatTime(answer.ticks) })}</li>
            <li>{t('击败 {{n}}', { n: answer.kills })}</li>
          </ul>
          {lines.length > 0 && <p className='df-lead'>{lines.join(' · ')}</p>}
          {!answer.won && (
            <p className='df-muted'>{t('这一场没打赢，用掉了一次挑战。')}</p>
          )}
          <div className='df-actions'>
            <Button theme='solid' onClick={() => onDone(answer)}>
              {t('回去')}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  if (phase === 'error') {
    return (
      <div className='df-panel'>
        <div className='df-card'>
          <div className='df-error'>{error}</div>
          <div className='df-actions'>
            {lastRun.current && (
              <Button theme='solid' onClick={() => submit(lastRun.current)}>
                {t('重新提交')}
              </Button>
            )}
            <Button onClick={() => onDone(null)}>{t('回去')}</Button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className='df-panel'>
      <div className='df-muted'>
        {phase === 'check' ? t('正在核对这一间房…') : t('正在接下…')}
      </div>
    </div>
  );
}

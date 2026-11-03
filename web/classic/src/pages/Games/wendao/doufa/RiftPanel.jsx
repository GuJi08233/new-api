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

import React, { useCallback, useEffect, useState } from 'react';
import { Button, Modal } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { gameGet, gamePost } from '../../common/api';
import { playSound } from '../../common/sound';
import ClassPicker from './ClassPicker';
import { encodeInputs } from './engine';
import Fight from './Fight';
import {
  getBlessingText,
  getRoomText,
  getWeaponNames,
  getWeaponText,
} from './text';
import './doufa.css';

const FLOOR_STEPS = 7;
const API = '/api/game/wendao/rift';

// 走了多深(成绩)换成"第几重第几步"。
const depth = (score) => ({
  floor: Math.floor(score / FLOOR_STEPS) + 1,
  step: score % FLOOR_STEPS,
});

// RiftPanel 是秘境斗法的一轮：选流派入秘境，每一步挑一间房；战斗房在浏览器里打完把操作记录交给服务端重放核对，
// 祝福与法宝在房间之间挑。一轮的状态都在服务端，刷新或换设备都能接着走(没打完的战斗房要从头打)。
export default function RiftPanel() {
  const { t } = useTranslation();
  const [state, setState] = useState(null);
  const [cls, setCls] = useState('warrior');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [fighting, setFighting] = useState(false);
  const [checking, setChecking] = useState(false);
  const [finished, setFinished] = useState(null);
  const rooms = getRoomText(t);
  const blessings = getBlessingText(t);
  const weaponNames = getWeaponNames(t);
  const weaponText = getWeaponText(t);

  const load = useCallback(async () => {
    const res = await gameGet(`${API}/state`, t);
    if (res.error) {
      setError(res.error);
      return;
    }
    setState(res.data);
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const session = state?.session;
  const run = session?.run;

  const act = async (body) => {
    setBusy(true);
    setError('');
    const res = await gamePost(
      `${API}/act`,
      { session_id: session.session_id, version: session.version, ...body },
      t,
    );
    setBusy(false);
    if (res.error) {
      setError(res.error);
      await load();
      return null;
    }
    const next = res.data.session;
    if (next.status === 'finished') {
      setFinished(next);
      setState((prev) => ({
        session: null,
        best: Math.max(prev?.best || 0, next.score),
      }));
    } else {
      setState((prev) => ({ ...prev, session: next }));
    }
    return next;
  };

  const start = async () => {
    setBusy(true);
    setError('');
    const res = await gamePost(`${API}/start`, { class: cls }, t);
    setBusy(false);
    if (res.error) {
      setError(res.error);
      await load();
      return;
    }
    setFinished(null);
    setState((prev) => ({ ...prev, session: res.data.session }));
    playSound('start');
  };

  const enter = async (choice) => {
    const next = await act({ action: 'enter', choice });
    if (next?.run?.room) setFighting(true);
  };

  const submitRoom = async (result) => {
    setChecking(true);
    const next = await act({
      action: 'clear',
      inputs: encodeInputs(result.inputs),
    });
    setChecking(false);
    setFighting(false);
    if (next && !next.run.over) playSound('coin');
  };

  const abandon = () =>
    Modal.confirm({
      title: t('放弃这一轮'),
      content: t('确定放弃这一轮？会按现在走到的深度结算。'),
      onOk: () => act({ action: 'abandon' }),
    });

  if (!state) {
    return (
      <div className='df-panel'>
        {error ? <div className='df-error'>{error}</div> : null}
      </div>
    );
  }

  const best = state.best ? depth(state.best) : null;

  if (!session) {
    const over = finished?.run;
    const reached = finished ? depth(finished.score) : null;
    return (
      <div className='df-panel'>
        {over && (
          <div className='df-card df-result'>
            <div className='df-title'>{t('这一轮结束')}</div>
            <ul className='df-stats'>
              <li>
                {t('走到第 {{floor}} 重第 {{step}} 步', {
                  floor: reached.floor,
                  step: reached.step + 1,
                })}
              </li>
              <li>{t('打通 {{n}} 间房', { n: over.cleared })}</li>
              <li>{t('击败 {{n}}', { n: over.kills })}</li>
              <li>{t('拾得灵石 {{n}}', { n: over.stones })}</li>
            </ul>
          </div>
        )}
        <div className='df-card'>
          <div className='df-title'>{t('斗法秘境')}</div>
          <p className='df-lead'>
            {t(
              '一重七步，每步从几间房里挑一间：战斗、精英、宝库、静修、祠堂，最后一步是首领。祝福只在这一轮里有效。每间房打完由服务器重放核对，现在只记成绩，不发游戏币。',
            )}
          </p>
          {best && (
            <p className='df-muted'>
              {t('最好成绩：第 {{floor}} 重第 {{step}} 步', {
                floor: best.floor,
                step: best.step + 1,
              })}
            </p>
          )}
          <ClassPicker value={cls} onChange={setCls} />
          {error && <div className='df-error'>{error}</div>}
          <div className='df-actions'>
            <Button theme='solid' loading={busy} onClick={start}>
              {t('入秘境')}
            </Button>
          </div>
        </div>
      </div>
    );
  }

  if (fighting && run.room) {
    return (
      <div className='df-panel'>
        <Fight
          key={run.room.seed}
          seed={run.room.seed}
          setup={run.room.setup}
          onEnd={submitRoom}
          onExit={() => setFighting(false)}
        />
        {checking && <div className='df-muted'>{t('正在核对这一间房…')}</div>}
      </div>
    );
  }

  const here = depth(session.score);
  return (
    <div className='df-panel'>
      <div className='df-card df-run'>
        <div className='df-run-head'>
          <span className='df-title'>
            {t('第 {{floor}} 重 · 第 {{step}}/{{total}} 步', {
              floor: run.floor,
              step: Math.min(run.step, FLOOR_STEPS - 1) + 1,
              total: FLOOR_STEPS,
            })}
          </span>
          <Button
            size='small'
            type='tertiary'
            disabled={busy}
            onClick={abandon}
          >
            {t('放弃这一轮')}
          </Button>
        </div>
        <div className='df-bars'>
          <div className='df-bar'>
            <span
              className='df-bar-fill is-hp'
              style={{ width: `${(100 * run.hp) / session.max_hp}%` }}
            />
            <span className='df-bar-text'>
              {t('生命 {{hp}}/{{max}}', { hp: run.hp, max: session.max_hp })}
            </span>
          </div>
          <span className='df-chip'>{t('灵力 {{n}}', { n: run.mp })}</span>
          <span className='df-chip'>{t('灵石 {{n}}', { n: run.stones })}</span>
          <span className='df-chip'>{t('击败 {{n}}', { n: run.kills })}</span>
        </div>
        <div className='df-tags'>
          <span className='df-tag-label'>{t('法宝')}</span>
          {run.weapons.map((w) => (
            <span key={w} className='df-tag'>
              {weaponNames[w]}
            </span>
          ))}
          <span className='df-tag-label'>{t('祝福')}</span>
          {run.blessings.length === 0 && (
            <span className='df-muted-inline'>—</span>
          )}
          {run.blessings.map((b) => (
            <span key={b} className='df-tag' title={blessings[b]?.desc}>
              {blessings[b]?.name || b}
            </span>
          ))}
        </div>
        {session.outdated && (
          <div className='df-error'>
            {t('这一局是按旧版规则开始的，不能继续，请放弃后结算')}
          </div>
        )}
      </div>

      {run.offer ? (
        <div className='df-card'>
          <div className='df-title'>
            {run.offer.kind === 'blessing' ? t('挑一样祝福') : t('挑一件法宝')}
          </div>
          <div className='df-choices'>
            {run.offer.items.map((item, i) =>
              run.offer.kind === 'blessing' ? (
                <button
                  key={item}
                  type='button'
                  className='df-choice'
                  disabled={busy}
                  onClick={() => act({ action: 'pick', choice: i })}
                >
                  <span className='df-choice-name'>
                    {blessings[item]?.name || item}
                  </span>
                  <span className='df-choice-desc'>
                    {blessings[item]?.desc}
                  </span>
                </button>
              ) : (
                <div key={item} className='df-choice is-static'>
                  <span className='df-choice-name'>{weaponNames[item]}</span>
                  <span className='df-choice-desc'>{weaponText[item]}</span>
                  <span className='df-choice-actions'>
                    {run.weapons.length < 2 ? (
                      <Button
                        size='small'
                        disabled={busy}
                        onClick={() =>
                          act({ action: 'pick', choice: i, slot: 0 })
                        }
                      >
                        {t('收下')}
                      </Button>
                    ) : (
                      run.weapons.map((w, slot) => (
                        <Button
                          key={w}
                          size='small'
                          disabled={busy}
                          onClick={() =>
                            act({ action: 'pick', choice: i, slot })
                          }
                        >
                          {t('换下{{name}}', { name: weaponNames[w] })}
                        </Button>
                      ))
                    )}
                  </span>
                </div>
              ),
            )}
          </div>
          <div className='df-actions'>
            <Button
              type='tertiary'
              disabled={busy}
              onClick={() => act({ action: 'pick', choice: -1 })}
            >
              {t('都不要')}
            </Button>
          </div>
        </div>
      ) : run.room ? (
        <div className='df-card'>
          <div className='df-title'>{rooms[run.room.kind]?.name}</div>
          <p className='df-muted'>{t('这一间还没打完，中途退出要从头打。')}</p>
          <div className='df-actions'>
            <Button theme='solid' onClick={() => setFighting(true)}>
              {t('进入战斗')}
            </Button>
          </div>
        </div>
      ) : (
        <div className='df-card'>
          <div className='df-title'>{t('选一间房进去')}</div>
          <div className='df-choices'>
            {(run.choices || []).map((kind, i) => (
              <button
                key={`${kind}-${i}`}
                type='button'
                className={`df-choice is-${kind}`}
                disabled={busy}
                onClick={() => enter(i)}
              >
                <span className='df-choice-name'>
                  {rooms[kind]?.name || kind}
                </span>
                <span className='df-choice-desc'>{rooms[kind]?.desc}</span>
              </button>
            ))}
          </div>
        </div>
      )}
      {error && <div className='df-error'>{error}</div>}
      {here.floor > 1 && run.step <= 0 && !run.offer && !run.room && (
        <p className='df-muted'>
          {t('第 {{n}} 重的敌人更强了。', { n: run.floor })}
        </p>
      )}
    </div>
  );
}

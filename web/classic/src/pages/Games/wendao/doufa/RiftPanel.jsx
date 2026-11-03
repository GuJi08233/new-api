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
import { Button, Dropdown, Modal, TabPane, Tabs } from '@douyinfe/semi-ui';
import { Backpack, Gem } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameShop from '../../common/GameShop';
import { StageMessage } from '../../common/SideBox';
import {
  gameGet,
  gamePost,
  gamePostOnce,
  withTurnstile,
} from '../../common/api';
import { playSkillFeedback, skillResultMessage } from '../../common/skill';
import { playSound } from '../../common/sound';
import { PlayTurnstile, usePlayTurnstile } from '../../common/usePlayTurnstile';
import { ITEM_ICONS } from '../../dungeon/parts';
import { getItemText } from '../../dungeon/text';
import ClassPicker from './ClassPicker';
import { encodeInputs } from './engine';
import Fight from './Fight';
import {
  getBlessingText,
  getRoomText,
  getTalentText,
  getWeaponNames,
  getWeaponText,
} from './text';
import './doufa.css';

const FLOOR_STEPS = 7;
const API = '/api/game/wendao/rift';
// 没有得到答复时隔这么久(毫秒)用同一个请求重发一次：版本号保证同一步不会走两遍。
const RETRY_DELAY = 800;

// 请求没有得到答复(断网、服务端暂时出错)时原样重发一次。上一次其实已经走完的，服务端认版本号：最后一步拿到同一次结算，
// 其余的被挡回来，页面重新读取就是现在的样子。
async function postRetrying(url, body, t) {
  const first = await gamePost(url, body, t);
  if (!first.network) return first;
  await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY));
  return gamePost(url, body, t);
}

// 标题旁的道韵数。
const Crystals = ({ n }) => (
  <span className='df-crystals'>
    <Gem size={14} />
    {n}
  </span>
);

// 一轮结束的结算：打到哪里、得到什么、游戏币发了没有。
const Result = ({ finished, symbol }) => {
  const { t } = useTranslation();
  const { run, result } = finished;
  const detail = result.detail || {};
  const message = skillResultMessage(result, t, symbol);
  const loot = detail.loot;
  return (
    <div className='df-card df-result'>
      <div className='df-title'>
        {detail.abandoned ? t('本轮已放弃') : t('倒在了秘境里')}
      </div>
      <ul className='df-stats'>
        <li>{t('打通 {{n}} 重', { n: finished.floors })}</li>
        <li>
          {t('走到第 {{floor}} 重第 {{step}} 步', {
            floor: run.floor,
            step: Math.max(run.step, 0) + 1,
          })}
        </li>
        <li>{t('打通 {{n}} 间房', { n: run.cleared })}</li>
        <li>{t('击败 {{n}}', { n: run.kills })}</li>
        <li>{t('拾得灵石 {{n}}', { n: run.stones })}</li>
        <li>{t('获得道韵 {{n}}', { n: detail.crystals ?? 0 })}</li>
      </ul>
      {loot && (
        <p className='df-muted'>
          {Object.values(loot).some((n) => n > 0)
            ? t('带回宗门：灵石 {{stone}} · 灵草 {{herb}} · 玄铁 {{ore}}', {
                stone: loot.stone || 0,
                herb: loot.herb || 0,
                ore: loot.ore || 0,
              })
            : t('今天带回宗门的材料已经够了，明天再来')}
        </p>
      )}
      <StageMessage kind={message.kind} note={message.note}>
        {message.text}
      </StageMessage>
    </div>
  );
};

// 营地的天赋页：用道韵升级天赋，天赋在开局时带进一轮。
const Talents = ({ info, busy, onBuy }) => {
  const { t } = useTranslation();
  const texts = getTalentText(t);
  const meta = info.meta;
  return (
    <div className='df-panel'>
      <div className='df-card'>
        <div className='df-title'>
          {t('天赋')}
          <Crystals n={meta.crystals} />
        </div>
        <p className='df-lead'>
          {t(
            '每轮结束按打通的房间和重数得到道韵。天赋在开局时带进一轮，已经开始的这一轮不受影响。',
          )}
        </p>
        <div className='df-talents'>
          {info.catalog.talents.map((talent) => {
            const level = meta.talents?.[talent.id] || 0;
            const maxed = level >= talent.costs.length;
            const cost = talent.costs[level];
            return (
              <div
                key={talent.id}
                className={`df-talent ${level > 0 ? 'is-owned' : ''}`}
              >
                <strong>
                  {texts[talent.id]?.name}
                  <small>
                    {level}/{talent.costs.length}
                  </small>
                </strong>
                <span>{texts[talent.id]?.desc}</span>
                <Button
                  size='small'
                  disabled={busy || maxed || meta.crystals < cost}
                  onClick={() => onBuy(talent.id)}
                >
                  {maxed ? t('已满级') : t('{{n}} 道韵', { n: cost })}
                </Button>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
};

// RiftPanel 是九幽秘境：营地里选流派、升天赋、买道具，入秘境后每一步挑一间房；战斗房在浏览器里打完把操作记录交给服务端
// 重放核对，祝福与法宝在房间之间挑。一轮的状态都在服务端，刷新或换设备都能接着走——但发出的战斗房只能打完，中途退出要用
// 一次退路重新开始，退路用完再退出就算倒下。打通几重按游戏币规则领奖，道韵攒起来升天赋。
export default function RiftPanel({ info, patch, reload, refreshAgenda }) {
  const { t } = useTranslation();
  const [state, setState] = useState(null);
  const [cls, setCls] = useState('warrior');
  const [tab, setTab] = useState('go');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  // 刚发出的房间(带种子与开局)：只有进房、用退路重来的回应里有，读取状态时拿不到。
  const [fightRoom, setFightRoom] = useState(null);
  const [checking, setChecking] = useState(false);
  const [finished, setFinished] = useState(null);
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const rooms = getRoomText(t);
  const blessings = getBlessingText(t);
  const weaponNames = getWeaponNames(t);
  const weaponText = getWeaponText(t);
  const itemTexts = getItemText(t);

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
  const meta = info.meta;

  // 一轮结束：记下结果、放音效，并让奖励进度、余额、道韵与待办跟着更新。
  const finish = (next) => {
    setFinished(next);
    setFightRoom(null);
    setState((prev) => ({
      session: null,
      best: Math.max(prev?.best || 0, next.floors),
    }));
    playSkillFeedback(next.result, next.floors > 0);
    patch({
      balance: next.result.balance,
      progress: next.result.progress,
      best_score: next.result.best_score,
    });
    reload(true);
    refreshAgenda();
  };

  const act = async (body) => {
    setBusy(true);
    setError('');
    const res = await postRetrying(
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
      finish(next);
      return next;
    }
    setState((prev) => ({ ...prev, session: next }));
    const room = next.run.room;
    setFightRoom(room && !room.hidden ? room : null);
    if (next.meta) patch({ meta: next.meta });
    return next;
  };

  const start = async () => {
    setBusy(true);
    setError('');
    const res = await gamePost(
      withTurnstile(`${API}/start`, turnstile.take()),
      { class: cls },
      t,
    );
    setBusy(false);
    if (res.error) {
      setError(res.error);
      await load();
      return;
    }
    setFinished(null);
    setFightRoom(null);
    setState((prev) => ({ ...prev, session: res.data.session }));
    playSound('start');
  };

  const buyTalent = async (id) => {
    setBusy(true);
    setError('');
    const res = await gamePost(`${API}/talent`, { id }, t);
    setBusy(false);
    if (res.error) {
      setError(res.error);
      return;
    }
    playSound('sparkle');
    patch({ meta: res.data.meta });
  };

  const buyItem = async (item, count) => {
    setBusy(true);
    setError('');
    const res = await gamePostOnce(`${API}/shop/buy`, { item, count }, t);
    setBusy(false);
    if (res.error) {
      setError(res.error);
      return;
    }
    playSound('coin');
    patch({ meta: res.data.meta, balance: res.data.balance });
  };

  const enter = (choice) => act({ action: 'enter', choice });

  const submitRoom = async (result) => {
    setChecking(true);
    const next = await act({
      action: 'clear',
      inputs: encodeInputs(result.inputs),
    });
    setChecking(false);
    // 没核对成功：房间已经发出去了，回到房间卡，要继续得用一次退路。
    if (!next) setFightRoom(null);
    else if (!next.run.over && !next.run.room) playSound('coin');
  };

  // 撤退：战斗中退出，或者读到一间已经进去过的房间。要用一次退路，没有了就算倒下。
  const retreat = () => {
    if (document.fullscreenElement) document.exitFullscreen?.();
    Modal.confirm({
      title: t('撤退'),
      content:
        run.retreats > 0
          ? t('撤退会用掉一次退路（还剩 {{n}} 次），这一间换个种子重新开始。', {
              n: run.retreats,
            })
          : t('已经没有退路了，撤退这一轮就结束了。'),
      onOk: () => act({ action: 'retreat' }),
    });
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

  if (fightRoom && run) {
    return (
      <div className='df-panel'>
        <Fight
          key={fightRoom.seed}
          seed={fightRoom.seed}
          setup={fightRoom.setup}
          onEnd={submitRoom}
          onExit={retreat}
          exitLabel={t('撤退')}
        />
        {checking && <div className='df-muted'>{t('正在核对这一间房…')}</div>}
        {error && <div className='df-error'>{error}</div>}
        <p className='df-muted'>
          {t('战斗中离开本页会用掉一次退路，退路还剩 {{n}} 次。', {
            n: run.retreats,
          })}
        </p>
      </div>
    );
  }

  if (!session) {
    return (
      <div className='df-panel'>
        {finished && <Result finished={finished} symbol={info.symbol} />}
        <Tabs type='line' activeKey={tab} onChange={setTab}>
          <TabPane itemKey='go' tab={t('出征')}>
            <div className='df-panel'>
              <div className='df-card'>
                <div className='df-title'>{t('九幽秘境')}</div>
                <p className='df-lead'>
                  {t(
                    '一重七步，每步从几间房里挑一间：战斗、精英、宝库、静修、祠堂，最后一步是首领。祝福只在这一轮里有效。每间房打完由服务器重放核对，按打通的重数领游戏币。',
                  )}
                </p>
                <p className='df-lead'>
                  {t(
                    '一间战斗房发出以后只能打完它。中途退出、刷新页面或换设备再进来，要用掉一次退路，这一间换个种子重新开始；退路用完再退出就算倒下。每轮有 {{n}} 次退路。',
                    { n: info.catalog.retreats },
                  )}
                </p>
                {state.best > 0 && (
                  <p className='df-muted'>
                    {t('最多打通 {{n}} 重', { n: state.best })}
                  </p>
                )}
                <ClassPicker value={cls} onChange={setCls} />
                {error && <div className='df-error'>{error}</div>}
                <div className='df-actions'>
                  <Button
                    theme='solid'
                    loading={busy}
                    disabled={!turnstile.ready}
                    onClick={start}
                  >
                    {t('入秘境')}
                  </Button>
                </div>
                <PlayTurnstile turnstile={turnstile} />
              </div>
            </div>
          </TabPane>
          <TabPane itemKey='talents' tab={t('天赋')}>
            <Talents info={info} busy={busy} onBuy={buyTalent} />
            {error && <div className='df-error'>{error}</div>}
          </TabPane>
          <TabPane itemKey='shop' tab={t('道具商店')}>
            <GameShop
              shop={info.shop}
              texts={itemTexts}
              icons={ITEM_ICONS}
              owned={meta.items}
              maxStack={info.catalog.max_item_stack}
              symbol={info.symbol}
              busy={busy}
              note={t(
                '道具在两间房之间用，用掉一件少一件；每轮最多用 {{n}} 件',
                {
                  n: info.catalog.items_per_run,
                },
              )}
              onBuy={buyItem}
            />
            {error && <div className='df-error'>{error}</div>}
          </TabPane>
        </Tabs>
      </div>
    );
  }

  // 背包里有的道具，按商店里的顺序；只能在两间房之间用。
  const bag = (info.shop?.items || [])
    .map((item) => item.id)
    .filter((id) => meta.items?.[id] > 0);
  const between = !run.room && !run.offer && !run.over;

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
          <span className='df-run-tools'>
            <Dropdown
              trigger='click'
              position='bottomRight'
              render={
                <Dropdown.Menu>
                  <Dropdown.Item disabled>
                    {t('本轮已用 {{used}} / {{max}} 件', {
                      used: run.used || 0,
                      max: info.catalog.items_per_run,
                    })}
                  </Dropdown.Item>
                  {bag.length === 0 && (
                    <Dropdown.Item disabled>
                      {t('背包是空的，可以在秘境入口的道具商店购买')}
                    </Dropdown.Item>
                  )}
                  {bag.map((id) => {
                    const Icon = ITEM_ICONS[id];
                    return (
                      <Dropdown.Item
                        key={id}
                        disabled={busy || !between}
                        icon={Icon && <Icon size={14} />}
                        onClick={() => act({ action: 'item', item: id })}
                      >
                        {itemTexts[id]?.name} × {meta.items[id]}
                      </Dropdown.Item>
                    );
                  })}
                </Dropdown.Menu>
              }
            >
              <Button size='small' icon={<Backpack size={14} />}>
                {t('道具')}
              </Button>
            </Dropdown>
            <Button
              size='small'
              type='tertiary'
              disabled={busy}
              onClick={abandon}
            >
              {t('放弃这一轮')}
            </Button>
          </span>
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
          <span className='df-chip'>
            {t('退路 {{n}}', { n: run.retreats })}
          </span>
        </div>
        <div className='df-tags'>
          <span className='df-tag-label'>{t('法宝')}</span>
          {run.weapons.map((w) => (
            <span key={w} className='df-tag'>
              {weaponNames[w]}
            </span>
          ))}
          <span className='df-tag-label'>{t('祝福')}</span>
          {run.blessings.length === 0 && !run.ward && (
            <span className='df-muted-inline'>—</span>
          )}
          {run.blessings.map((b) => (
            <span key={b} className='df-tag' title={blessings[b]?.desc}>
              {blessings[b]?.name || b}
            </span>
          ))}
          {run.ward && (
            <span className='df-tag is-ward' title={itemTexts.ward?.desc}>
              {itemTexts.ward?.name}
            </span>
          )}
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
          <p className='df-muted'>
            {run.retreats > 0
              ? t(
                  '这一间已经进去过了，重新进入要用掉一次退路（还剩 {{n}} 次），敌人不变，随机重新来过。',
                  { n: run.retreats },
                )
              : t(
                  '这一间已经进去过了，退路也用完了：现在只能认输，这一轮按打通的重数结算。',
                )}
          </p>
          <div className='df-actions'>
            <Button theme='solid' disabled={busy} onClick={retreat}>
              {run.retreats > 0 ? t('重新进入') : t('认输')}
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
                disabled={busy || session.outdated}
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
      {run.floor > 1 && run.step <= 0 && !run.offer && !run.room && (
        <p className='df-muted'>
          {t('第 {{n}} 重的敌人更强了。', { n: run.floor })}
        </p>
      )}
    </div>
  );
}

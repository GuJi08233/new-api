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
import { TabPane, Tabs, Toast } from '@douyinfe/semi-ui';
import { Gem, Hammer, Leaf, Star } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { playSkillFeedback, SkillSide } from '../common/skill';
import { Floaters, celebrate, useFloaters } from '../common/effects';
import { playSound } from '../common/sound';
import { gameGet, gamePost, gamePostOnce, withTurnstile } from '../common/api';
import { WendaoLayout, useWendaoPart, useWendaoShell } from '../wendao/shell';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import { useVisiblePolling } from '../common/useVisiblePolling';
import Overview from './Overview';
import Disciples from './Disciples';
import Missions from './Missions';
import Outposts from './Outposts';
import Alchemy from './Alchemy';
import Tournament from './Tournament';
import Shop from './Shop';
import Arena from './Arena';
import Alliance from './Alliance';
import Garden from './Garden';
import Market from './Market';
import PathPanel from './PathPanel';
import { dailyPending } from './DailyPanel';
import {
  SectTextProvider,
  eventSentence,
  formatAmount,
  pillName,
  useSectText,
} from './parts';
import { PILLS, RARES } from './text';
import './sect.css';

const RESOURCE_BAR = [
  { key: 'stone', Icon: Gem },
  { key: 'herb', Icon: Leaf },
  { key: 'ore', Icon: Hammer },
  { key: 'rep', Icon: Star },
];

// 两次同步之间页面自己走时钟：倒计时、修为条与资源按服务端给的速度往前推，每分钟再与服务端同步一次。
const SYNC_INTERVAL = 60 * 1000;

function useServerClock(serverNow, receivedAt) {
  const [, setTick] = useState(0);
  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  if (!serverNow) return { now: 0, elapsed: 0 };
  const elapsed = Math.max(0, (Date.now() - receivedAt) / 1000);
  return { now: Math.floor(serverNow + elapsed), elapsed };
}

const SectPage = () => {
  const { t } = useTranslation();
  const text = useSectText();
  const { info, patch, reload } = useWendaoPart('sect');
  const { focus, refreshAgenda } = useWendaoShell();
  const focused = focus?.part === 'sect' ? focus : null;
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [sect, setSect] = useState(null);
  const [outposts, setOutposts] = useState(null);
  // standing 是主角的身份({ identity, path, realm, stipend_ready })：宗门操作与身份操作的回应里带着，商店这些不带的
  // 回应不改它。
  const [standing, setStanding] = useState(null);
  const [receivedAt, setReceivedAt] = useState(0);
  const [tab, setTab] = useState(focused?.tab || 'overview');
  const [round, setRound] = useState(null);
  const [arenaRound, setArenaRound] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [floaters, addFloater] = useFloaters();
  // 最近一次操作的结果写成几行字留在台面上：飘字跟随“减少动态效果”可能不播放，结果要始终看得到。
  const [notice, setNotice] = useState(null);
  const busyRef = useRef(false);
  const { now, elapsed } = useServerClock(sect?.now, receivedAt);

  // 一次操作里发生的事：资源与丹药的增减飘字，重要的事写成一句话飘出来，并配上音效。一键收获、一键结算这类一次办几件
  // 的操作，资源与丹药按种类合计成一笔。
  const showEvents = useCallback(
    (events) => {
      const lines = [];
      const gains = [];
      // totals 按出现的先后记每种资源、灵药、丹药的合计：资源写成「+60 灵石」，灵药与丹药写成「回春丹 +2」。
      const totals = new Map();
      let row = 0;
      const float = (line, kind) => {
        addFloater(line, { x: 50, y: 18 + row * 8, kind });
        row += 1;
      };
      for (const ev of events) {
        switch (ev.k) {
          case 'res':
          case 'rare':
          case 'pill': {
            const key = `${ev.k}:${ev.s}`;
            const total = totals.get(key) || { kind: ev.k, s: ev.s, n: 0 };
            total.n += ev.n;
            totals.set(key, total);
            break;
          }
          case 'harvest':
            playSound(ev.n === 2 ? 'win' : 'coin');
            float(eventSentence(ev, text, t), 'gold');
            break;
          case 'garden_water':
          case 'garden_pick':
          case 'daily':
            playSound('sparkle');
            break;
          case 'daily_chest':
            playSound('win');
            celebrate(1);
            break;
          case 'realm':
            playSound('win');
            celebrate(1);
            float(eventSentence(ev, text, t), 'gain');
            break;
          case 'break_fail':
          case 'injured':
            playSound('lose');
            float(eventSentence(ev, text, t), 'bad');
            break;
          case 'outpost_build':
          case 'outpost_station':
          case 'outpost_recall':
          case 'built':
          case 'joined':
          case 'talent':
            playSound('sparkle');
            float(eventSentence(ev, text, t), 'gain');
            break;
          case 'mission':
            playSound(ev.n > 0 ? 'coin' : 'lose');
            float(eventSentence(ev, text, t), ev.n > 0 ? 'gold' : 'bad');
            break;
          case 'bottleneck':
          case 'layer':
            float(eventSentence(ev, text, t), 'gain');
            break;
          case 'path_found':
            playSound('win');
            celebrate(1);
            float(eventSentence(ev, text, t), 'gain');
            break;
          case 'path_join':
          case 'path_rank':
            playSound('sparkle');
            float(eventSentence(ev, text, t), 'gain');
            break;
          case 'path_stipend':
          case 'path_exchange':
            playSound('coin');
            break;
          case 'item':
            playSound('sparkle');
            lines.push(t('使用：{{name}}', { name: text.items[ev.s]?.name }));
            float(
              t('使用：{{name}}', { name: text.items[ev.s]?.name }),
              'gain',
            );
            break;
          default:
        }
        if (!['res', 'pill', 'rare', 'layer'].includes(ev.k)) {
          const line = eventSentence(ev, text, t);
          if (line) lines.push(line);
        }
      }
      for (const total of totals.values()) {
        if (total.n === 0) continue;
        const amount = `${total.n > 0 ? '+' : ''}${total.n}`;
        const gain =
          total.kind === 'res'
            ? `${amount} ${text.resources[total.s]}`
            : `${total.kind === 'pill' ? pillName(text, total.s) : text.rares[total.s]} ${amount}`;
        gains.push(gain);
        float(
          gain,
          total.n < 0 ? 'bad' : total.kind === 'res' ? 'gold' : 'gain',
        );
      }
      if (lines.length || gains.length) {
        setNotice({ lines, gains: gains.join(' · ') });
      }
    },
    [addFloater, t, text],
  );

  const accept = useCallback((data) => {
    setSect(data.sect);
    if (data.standing) setStanding(data.standing);
    if ('outposts' in data) setOutposts(data.outposts);
    setReceivedAt(Date.now());
  }, []);

  // 从顶上的待办点过来：打开要去的页签。
  useEffect(() => {
    if (focused?.tab) setTab(focused.tab);
  }, [focused]);

  // writes 是发出过几次操作：定时同步发出以后又有操作发出，同步的回应就可能比操作的旧，不拿它覆盖。send 默认是
  // gamePost；经营操作与商店购买用 gamePostOnce，网络断了重发不会做两次。
  const writes = useRef(0);
  const post = async (url, body, send = gamePost) => {
    if (busyRef.current) return null;
    busyRef.current = true;
    writes.current += 1;
    setBusy(true);
    setError('');
    try {
      const response = await send(url, body, t);
      if (response.data) {
        refreshAgenda();
        if (response.data.replayed) {
          Toast.info(t('网络刚才断了一下，这一步上次已经做完了'));
        }
        return response.data;
      }
      playSound('error');
      setError(response.error);
      return null;
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const act = async (action, fields = {}) => {
    setNotice(null);
    const data = await post(
      '/api/game/wendao/sect/action',
      { action, ...fields },
      gamePostOnce,
    );
    if (!data) return false;
    accept(data);
    showEvents(data.events || []);
    return true;
  };

  const actOutpost = async (action, fields = {}) => {
    setNotice(null);
    const data = await post(
      '/api/game/wendao/sect/outpost',
      { action, ...fields },
      gamePostOnce,
    );
    if (!data) {
      await sync();
      return false;
    }
    accept(data);
    showEvents(data.events || []);
    return true;
  };

  // 身份操作：拜入、退出、开宗立派、领俸禄、上交、换宝库。开宗以后页签换成宗门经营的那一套。
  const actPath = async (action, fields = {}) => {
    setNotice(null);
    const data = await post(
      '/api/game/wendao/sect/path',
      { action, ...fields },
      gamePostOnce,
    );
    if (!data) return false;
    accept(data);
    showEvents(data.events || []);
    return true;
  };

  const buyItem = async (item, count) => {
    setNotice(null);
    const data = await post(
      '/api/game/wendao/sect/shop/buy',
      { item, count },
      gamePostOnce,
    );
    if (!data) return;
    playSound('coin');
    accept(data);
    patch({ balance: data.balance });
  };

  const sync = useCallback(async () => {
    if (busyRef.current) return;
    const at = writes.current;
    const response = await gamePost(
      '/api/game/wendao/sect/action',
      { action: 'sync' },
      t,
    );
    if (writes.current !== at) return;
    if (response.data) {
      accept(response.data);
      showEvents(response.data.events || []);
    }
  }, [accept, showEvents, t]);

  // 打开页面时同步储物处(第一次会建一个)，并读出进行中的大比。
  const loaded = !!info;
  useEffect(() => {
    if (!loaded) return;
    sync();
    gameGet('/api/game/wendao/sect/tournament', t).then((response) => {
      const session = response.data?.session;
      if (session?.status === 'playing') {
        setRound(session);
        setTab('tournament');
      }
    });
    gameGet('/api/game/wendao/sect/arena/battle', t).then((response) => {
      const session = response.data?.session;
      if (session?.status === 'playing') {
        setArenaRound(session);
        setTab('arena');
      }
    });
    // 只在第一次读到游戏信息时执行。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded]);
  useVisiblePolling(sync, SYNC_INTERVAL, loaded && !!sect);

  const startTournament = async (team) => {
    const data = await post(
      withTurnstile('/api/game/wendao/sect/tournament/start', turnstile.take()),
      { team },
    );
    if (!data) return;
    playSound('start');
    setRound(data);
    sync();
  };

  const move = async (action, index = 0) => {
    if (!round || round.status !== 'playing') return;
    const data = await post('/api/game/wendao/sect/tournament/action', {
      session_id: round.session_id,
      version: round.version,
      action,
      index,
    });
    if (!data) {
      const response = await gameGet(
        `/api/game/wendao/sect/tournament?session_id=${round.session_id}`,
        t,
      );
      if (response.data?.session) setRound(response.data.session);
      return;
    }
    setRound(data);
    const last = data.tournament.last;
    if (action !== 'pick' && last) {
      playSound(last.dealt + last.counter > 0 ? 'hit' : 'click');
    }
    if (data.result) {
      patch({
        balance: data.result.balance,
        progress: data.result.progress,
        best_score: data.result.best_score,
      });
      playSkillFeedback(data.result, data.tournament.wins >= 3);
      reload(true);
      sync();
    }
  };

  const backFromTournament = () => setRound(null);

  const res = sect?.res || {};
  const rates = sect?.rates || {};
  // 资源按每小时的产出往前推，到下一次同步时由服务端校准。
  const projected = (key) =>
    key === 'rep'
      ? res.rep || 0
      : (res[key] || 0) + ((rates[key] || 0) * elapsed) / 3600;
  const readyMissions = sect
    ? sect.missions.filter((m) => m.end <= now).length
    : 0;
  const pills = sect?.pills || {};
  const rares = sect?.rare || {};
  const ripePlots = sect
    ? sect.garden.plots.filter((plot) => plot.crop && plot.ripe <= now).length
    : 0;
  const founded = !!sect?.founded;
  // 灵田与商店两个页签开没开宗都有：没开宗时灵田只能去道友那里串门，商店只卖丹药。
  const gardenPane = (
    <TabPane
      tab={ripePlots > 0 ? `${t('灵田')} ●` : t('灵田')}
      itemKey='garden'
    >
      {sect && (
        <Garden
          sect={sect}
          now={now}
          busy={busy}
          act={act}
          post={post}
          onEvents={showEvents}
          onSync={sync}
        />
      )}
    </TabPane>
  );
  const shopPane = (
    <TabPane tab={t('商店')} itemKey='shop'>
      {sect && (
        <Shop info={info} sect={sect} busy={busy} act={act} onBuy={buyItem} />
      )}
    </TabPane>
  );

  return (
    <WendaoLayout
      loading={loaded && !sect && !error}
      className='sc-page'
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('大比连胜')}
            extra={
              <>
                {founded && (
                  <SideBox
                    label={t('宗门战绩')}
                    value={sect.stats.best_rounds}
                    note={t(
                      '大比 {{n}} 次 · 夺魁 {{champions}} 次 · 突破 {{breaks}} 次',
                      {
                        n: sect.stats.tournaments,
                        champions: sect.stats.champions,
                        breaks: sect.stats.breakthroughs,
                      },
                    )}
                  />
                )}
                <SideBox label={t('玩法说明')}>
                  {sect && !founded ? (
                    <RuleList
                      items={[
                        t(
                          '散修无门无派：历练、闭关与秘境照常，资源放在自己的储物处',
                        ),
                        t(
                          '拜入正道宗门：每天领俸禄，上交资源或做每日功课攒贡献，在宗门宝库换丹药与灵药；累计贡献与境界够了自动晋升',
                        ),
                        t(
                          '每个宗门有一样门派加成；退出宗门身份与贡献清零，七天内不能再拜入',
                        ),
                        t(
                          '修到{{realm}}、攒够 {{n}} 灵石可以开宗立派：建起主殿与灵田，收下两名弟子，之后经营自己的宗门',
                          {
                            realm: text.realms[info.path?.found_realm],
                            n: info.path?.found_cost,
                          },
                        ),
                        t(
                          '游戏币只来自宗门大比与秘境；商店里丹药都能买，符箓只对自己的宗门有用，开宗以后才卖',
                        ),
                      ]}
                    />
                  ) : (
                    <RuleList
                      items={[
                        t(
                          '建筑每小时产出灵石、灵草与玄铁，弟子按资质与性格闭关修炼；离线最多结算 12 小时',
                        ),
                        t(
                          '派弟子外出历练带回资源与丹药，处理奇遇；修到第九层圆满要突破，失败会受伤',
                        ),
                        t(
                          '灵田种下灵种，熟了收获灵草、玄铁、丹药或灵药；可以去道友的灵田浇灌、顺手摘取',
                        ),
                        t(
                          '游戏币只来自宗门大比：按连胜场数领奖，挂机产出的资源不能换游戏币',
                        ),
                        t(
                          '论剑：设好守擂阵容登上论剑榜，挑战别的宗门按积分排名；仙盟：结盟捐资源升级，每周合力讨伐妖王',
                        ),
                        t(
                          '万宝阁：掌门之间用灵石买卖灵草、玄铁、丹药与灵药，也能指名赠送道友',
                        ),
                        t(
                          '商店里可以用游戏币买一次性的符箓与丹药，花掉的游戏币不退回',
                        ),
                        t('宗门存档保存在服务器上，换设备也能继续'),
                      ]}
                    />
                  )}
                </SideBox>
              </>
            }
          />
        )
      }
    >
      <div className='games-stage sc-stage'>
        {sect && (
          <div className='sc-resources'>
            {RESOURCE_BAR.map(({ key, Icon }) => (
              <span key={key} className={`sc-res is-${key}`}>
                <Icon size={16} />
                <span className='sc-res-name'>{text.resources[key]}</span>
                <strong>{formatAmount(projected(key))}</strong>
                {key !== 'rep' && rates[key] > 0 && (
                  <span className='sc-muted'>
                    +{formatAmount(rates[key])}/{t('时')}
                  </span>
                )}
              </span>
            ))}
            <span className='sc-res is-pills'>
              {PILLS.filter(
                (pill) => pill !== 'peiyuan' || pills.peiyuan > 0,
              ).map((pill) => (
                <span key={pill} className='sc-pill-count'>
                  {pillName(text, pill)} {pills[pill] || 0}
                </span>
              ))}
              {RARES.filter((rare) => rares[rare] > 0).map((rare) => (
                <span key={rare} className='sc-pill-count is-rare'>
                  {text.rares[rare]} {rares[rare]}
                </span>
              ))}
            </span>
          </div>
        )}
        {notice && (
          <div className='sc-notice'>
            <div>
              {notice.lines.map((line, index) => (
                <p key={index}>{line}</p>
              ))}
              {notice.gains && <p className='sc-muted'>{notice.gains}</p>}
            </div>
            <button
              type='button'
              className='sc-notice-close'
              aria-label={t('关闭')}
              onClick={() => setNotice(null)}
            >
              ×
            </button>
          </div>
        )}
        <div className='sc-body'>
          {sect && !founded && (
            <Tabs
              type='button'
              activeKey={['garden', 'shop'].includes(tab) ? tab : 'overview'}
              onChange={setTab}
              size='small'
            >
              <TabPane
                tab={
                  standing?.stipend_ready || dailyPending(sect.daily)
                    ? `${t('身份')} ●`
                    : t('身份')
                }
                itemKey='overview'
              >
                <PathPanel
                  rules={info?.path}
                  sect={sect}
                  standing={standing}
                  now={now}
                  busy={busy}
                  act={act}
                  actPath={actPath}
                  onDuelDone={(answer) => {
                    accept(answer);
                    showEvents(answer.events || []);
                    refreshAgenda();
                  }}
                />
              </TabPane>
              {gardenPane}
              {shopPane}
            </Tabs>
          )}
          {founded && (
            <Tabs type='button' activeKey={tab} onChange={setTab} size='small'>
              <TabPane
                tab={
                  sect.event || dailyPending(sect.daily)
                    ? `${t('宗门')} ●`
                    : t('宗门')
                }
                itemKey='overview'
              >
                <Overview sect={sect} now={now} busy={busy} act={act} />
              </TabPane>
              <TabPane
                tab={t('弟子 {{n}}', { n: sect.disciples.length })}
                itemKey='disciples'
              >
                <Disciples
                  sect={sect}
                  now={now}
                  elapsed={elapsed}
                  busy={busy}
                  act={act}
                />
              </TabPane>
              <TabPane tab={t('下院')} itemKey='outposts'>
                <Outposts
                  outposts={outposts}
                  sect={sect}
                  standing={standing}
                  now={now}
                  busy={busy}
                  act={actOutpost}
                  focus={focused}
                />
              </TabPane>
              <TabPane
                tab={readyMissions > 0 ? `${t('派遣')} ●` : t('派遣')}
                itemKey='missions'
              >
                <Missions sect={sect} now={now} busy={busy} act={act} />
              </TabPane>
              {gardenPane}
              <TabPane tab={t('丹房')} itemKey='alchemy'>
                <Alchemy sect={sect} busy={busy} act={act} />
              </TabPane>
              <TabPane
                tab={round?.status === 'playing' ? `${t('大比')} ●` : t('大比')}
                itemKey='tournament'
              >
                <Tournament
                  info={info}
                  sect={sect}
                  now={now}
                  round={round}
                  busy={busy}
                  turnstile={turnstile}
                  onStart={startTournament}
                  onMove={move}
                  onBack={backFromTournament}
                />
              </TabPane>
              <TabPane
                tab={
                  arenaRound?.status === 'playing'
                    ? `${t('论剑')} ●`
                    : t('论剑')
                }
                itemKey='arena'
              >
                <Arena
                  sect={sect}
                  now={now}
                  busy={busy}
                  post={post}
                  round={arenaRound}
                  setRound={setArenaRound}
                  onSettled={sync}
                />
              </TabPane>
              <TabPane tab={t('仙盟')} itemKey='alliance'>
                <Alliance
                  sect={sect}
                  now={now}
                  busy={busy}
                  post={post}
                  onSect={(next) => accept({ sect: next })}
                  onEvents={showEvents}
                />
              </TabPane>
              <TabPane tab={t('万宝阁')} itemKey='market'>
                <Market
                  sect={sect}
                  busy={busy}
                  post={post}
                  onSect={(next) => accept({ sect: next })}
                />
              </TabPane>
              {shopPane}
            </Tabs>
          )}
          <Floaters items={floaters} />
        </div>
        {error && <StageMessage kind='error'>{error}</StageMessage>}
      </div>
    </WendaoLayout>
  );
};

// 文案表要在组件树里先提供，页面本身也要用到(资源名、飘字)。
const Sect = () => (
  <SectTextProvider>
    <SectPage />
  </SectTextProvider>
);

export default Sect;

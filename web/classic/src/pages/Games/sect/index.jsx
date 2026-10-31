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
import { TabPane, Tabs } from '@douyinfe/semi-ui';
import { Gem, Hammer, Leaf, Star } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import GameLayout from '../common/GameLayout';
import { RuleList, SideBox, StageMessage } from '../common/SideBox';
import { playSkillFeedback, SkillSide } from '../common/skill';
import { Floaters, celebrate, useFloaters } from '../common/effects';
import { playSound } from '../common/sound';
import { gameGet, gamePost, withTurnstile } from '../common/api';
import { useGameInfo } from '../common/useGameInfo';
import { usePlayTurnstile } from '../common/usePlayTurnstile';
import { useVisiblePolling } from '../common/useVisiblePolling';
import Overview from './Overview';
import Disciples from './Disciples';
import Missions from './Missions';
import Alchemy from './Alchemy';
import Tournament from './Tournament';
import {
  SectTextProvider,
  eventSentence,
  formatAmount,
  pillName,
  useSectText,
} from './parts';
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
  const { info, loading, unavailable, patch, reload } = useGameInfo('sect');
  const turnstile = usePlayTurnstile(info?.play_turnstile);
  const [sect, setSect] = useState(null);
  const [receivedAt, setReceivedAt] = useState(0);
  const [tab, setTab] = useState('overview');
  const [round, setRound] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [floaters, addFloater] = useFloaters();
  // 最近一次操作的结果写成几行字留在台面上：飘字跟随“减少动态效果”可能不播放，结果要始终看得到。
  const [notice, setNotice] = useState(null);
  const busyRef = useRef(false);
  const { now, elapsed } = useServerClock(sect?.now, receivedAt);

  // 一次操作里发生的事：资源与丹药的增减飘字，重要的事写成一句话飘出来，并配上音效。
  const showEvents = useCallback(
    (events) => {
      const lines = [];
      const gains = [];
      let row = 0;
      const float = (line, kind) => {
        addFloater(line, { x: 50, y: 18 + row * 8, kind });
        row += 1;
      };
      for (const ev of events) {
        switch (ev.k) {
          case 'res':
            gains.push(`${ev.n > 0 ? '+' : ''}${ev.n} ${text.resources[ev.s]}`);
            float(
              `${ev.n > 0 ? '+' : ''}${ev.n} ${text.resources[ev.s]}`,
              ev.n > 0 ? 'gold' : 'bad',
            );
            break;
          case 'pill':
            gains.push(`${pillName(text, ev.s)} ${ev.n > 0 ? '+' : ''}${ev.n}`);
            float(
              `${pillName(text, ev.s)} ${ev.n > 0 ? '+' : ''}${ev.n}`,
              ev.n > 0 ? 'gain' : 'bad',
            );
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
          default:
        }
        if (ev.k !== 'res' && ev.k !== 'pill' && ev.k !== 'layer') {
          const line = eventSentence(ev, text, t);
          if (line) lines.push(line);
        }
      }
      if (lines.length || gains.length) {
        setNotice({ lines, gains: gains.join(' · ') });
      }
    },
    [addFloater, t, text],
  );

  const accept = useCallback((data) => {
    setSect(data.sect);
    setReceivedAt(Date.now());
  }, []);

  const post = async (url, body) => {
    if (busyRef.current) return null;
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const response = await gamePost(url, body, t);
      if (response.data) return response.data;
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
    const data = await post('/api/game/sect/action', { action, ...fields });
    if (!data) return false;
    accept(data);
    showEvents(data.events || []);
    return true;
  };

  const sync = useCallback(async () => {
    if (busyRef.current) return;
    const response = await gamePost(
      '/api/game/sect/action',
      { action: 'sync' },
      t,
    );
    if (response.data) {
      accept(response.data);
      showEvents(response.data.events || []);
    }
  }, [accept, showEvents, t]);

  // 打开页面时同步宗门(第一次会开宗立派)，并读出进行中的大比。
  const loaded = !!info;
  useEffect(() => {
    if (!loaded) return;
    sync();
    gameGet('/api/game/sect/tournament', t).then((response) => {
      const session = response.data?.session;
      if (session?.status === 'playing') {
        setRound(session);
        setTab('tournament');
      }
    });
    // 只在第一次读到游戏信息时执行。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loaded]);
  useVisiblePolling(sync, SYNC_INTERVAL, loaded && !!sect);

  const startTournament = async (team) => {
    const data = await post(
      withTurnstile('/api/game/sect/tournament/start', turnstile.take()),
      { team },
    );
    if (!data) return;
    playSound('start');
    setRound(data);
    sync();
  };

  const move = async (action, index = 0) => {
    if (!round || round.status !== 'playing') return;
    const data = await post('/api/game/sect/tournament/action', {
      session_id: round.session_id,
      version: round.version,
      action,
      index,
    });
    if (!data) {
      const response = await gameGet(
        `/api/game/sect/tournament?session_id=${round.session_id}`,
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

  return (
    <GameLayout
      title={t('问道宗门')}
      loading={loading || (loaded && !sect && !error)}
      unavailable={unavailable}
      balance={info?.balance}
      symbol={info?.symbol}
      className='sc-page'
      side={
        info && (
          <SkillSide
            info={info}
            symbol={info.symbol}
            scoreName={t('大比连胜')}
            extra={
              <>
                {sect && (
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
                  <RuleList
                    items={[
                      t(
                        '建筑每小时产出灵石、灵草与玄铁，弟子按资质与性格闭关修炼；离线最多结算 12 小时',
                      ),
                      t(
                        '派弟子外出历练带回资源与丹药，处理奇遇；修到第九层圆满要突破，失败会受伤',
                      ),
                      t(
                        '游戏币只来自宗门大比：按连胜场数领奖，挂机产出的资源不能换游戏币',
                      ),
                      t('宗门存档保存在服务器上，换设备也能继续'),
                    ]}
                  />
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
              {['heal', 'qi', 'break', 'marrow'].map((pill) => (
                <span key={pill} className='sc-pill-count'>
                  {pillName(text, pill)} {pills[pill] || 0}
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
          {sect && (
            <Tabs type='button' activeKey={tab} onChange={setTab} size='small'>
              <TabPane
                tab={sect.event ? `${t('宗门')} ●` : t('宗门')}
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
              <TabPane
                tab={readyMissions > 0 ? `${t('历练')} ●` : t('历练')}
                itemKey='missions'
              >
                <Missions sect={sect} now={now} busy={busy} act={act} />
              </TabPane>
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
            </Tabs>
          )}
          <Floaters items={floaters} />
        </div>
        {error && <StageMessage kind='error'>{error}</StageMessage>}
      </div>
    </GameLayout>
  );
};

// 文案表要在组件树里先提供，页面本身也要用到(资源名、飘字)。
const Sect = () => (
  <SectTextProvider>
    <SectPage />
  </SectTextProvider>
);

export default Sect;

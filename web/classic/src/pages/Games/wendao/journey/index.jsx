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
import { useTranslation } from 'react-i18next';
import { RuleList, SideBox, StageMessage } from '../../common/SideBox';
import { gameGet, gamePost } from '../../common/api';
import { playSound } from '../../common/sound';
import { celebrate } from '../../common/effects';
import { useVisiblePolling } from '../../common/useVisiblePolling';
import { DungeonTextProvider, useDungeonText } from '../../dungeon/parts';
import { SectTextProvider, useSectText } from '../../sect/parts';
import { WendaoLayout, useWendaoPart } from '../shell';
import CreateHero from './CreateHero';
import HeroCard from './HeroCard';
import MapPanel from './MapPanel';
import StepsPanel from './StepsPanel';
import Collection from './Collection';
import Gear from './Gear';
import Cave from '../cave/Cave';
import BattleStage from './BattleStage';
import StoryDialog from './StoryDialog';
import { heroEventLines } from './lines';
import '../../common/logic-puzzles.css';
import '../../dungeon/dungeon.css';
import '../../sect/sect.css';
import './journey.css';

// 两次同步之间页面自己推进修为条，每分钟再与服务端同步一次。
const SYNC_INTERVAL = 60 * 1000;

const JourneyPage = () => {
  const { t } = useTranslation();
  const text = useDungeonText();
  const sectText = useSectText();
  const { info } = useWendaoPart('journey');
  const [state, setState] = useState(null);
  const [receivedAt, setReceivedAt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [tab, setTab] = useState('map');
  const [selected, setSelected] = useState(null);
  const [notice, setNotice] = useState(null);
  const [ended, setEnded] = useState(null);
  const [battleEvents, setBattleEvents] = useState(null);
  // story 是正在播放的一段剧情：lines、chapter、title、doneLabel 与播完后做什么(onDone)。
  const [story, setStory] = useState(null);
  const busyRef = useRef(false);
  const eventKey = useRef(0);
  const [, setTick] = useState(0);

  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  const accept = useCallback((data) => {
    setState({
      hero: data.hero,
      res: data.res,
      pills: data.pills,
      rare: data.rare,
    });
    setReceivedAt(Date.now());
    const events = data.hero?.battle?.run?.events;
    if (events?.length) {
      eventKey.current += 1;
      setBattleEvents({ key: eventKey.current, list: events });
    }
  }, []);

  const sync = useCallback(async () => {
    const response = await gameGet('/api/game/wendao/hero', t);
    if (response.data) {
      accept(response.data);
      setError('');
    } else {
      setError(response.error);
    }
  }, [accept, t]);

  useEffect(() => {
    sync();
  }, [sync]);
  useVisiblePolling(sync, SYNC_INTERVAL, !!state?.hero && !state.hero.battle);

  const act = async (action, fields = {}) => {
    if (busyRef.current) return null;
    busyRef.current = true;
    setBusy(true);
    setError('');
    try {
      const response = await gamePost(
        '/api/game/wendao/hero/action',
        { action, ...fields },
        t,
      );
      if (!response.data) {
        playSound('error');
        setError(response.error);
        if (response.network) sync();
        return null;
      }
      const data = response.data;
      accept(data);
      if (data.ended) {
        const won = data.ended.result === 'won';
        playSound(won ? 'win' : 'lose');
        const milestone = data.events.some(
          (ev) =>
            ev.k === 'hero_key' || (ev.k === 'hero_step' && ev.n % 10 === 0),
        );
        if (won && milestone) celebrate(1);
        setEnded({
          run: data.ended,
          events: data.events,
          fled: action === 'flee',
        });
      } else if (action !== 'battle' && data.events.length) {
        const summary = heroEventLines(data.events, t, text, sectText);
        if (summary.lines.length || summary.gains.length) setNotice(summary);
        const milestone = data.events.some(
          (ev) => ev.k === 'hero_realm' || ev.k === 'hero_steps_open',
        );
        if (milestone) celebrate(1);
        else if (data.events.some((ev) => ev.k === 'hero_break_fail'))
          playSound('lose');
        else playSound('sparkle');
      }
      return data;
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const battle = (move, fields = {}) =>
    act('battle', {
      move,
      card: fields.card ?? -1,
      target: fields.target ?? -1,
      choice: fields.choice ?? -1,
      potion: fields.potion ?? -1,
    });

  const hero = state?.hero;
  const elapsed = Math.max(0, (Date.now() - receivedAt) / 1000);
  const now = hero ? Math.floor(hero.now + elapsed) : 0;

  let body;
  if (!state) {
    body = null;
  } else if (story) {
    body = (
      <StoryDialog
        key={story.key}
        lines={story.lines}
        chapter={story.chapter}
        title={story.title}
        doneLabel={story.doneLabel}
        onDone={async () => {
          const next = story.onDone;
          setStory(null);
          if (next) await next();
        }}
      />
    );
  } else if (!hero) {
    body = (
      <CreateHero
        busy={busy}
        onCreate={async (cls) => {
          const data = await act('create', { class: cls });
          if (data) playSound('start');
        }}
      />
    );
  } else if (hero.battle || ended) {
    body = (
      <BattleStage
        info={info}
        hero={hero}
        busy={busy}
        ended={ended}
        events={battleEvents}
        send={battle}
        onFlee={() => act('flee')}
        onBack={() => setEnded(null)}
      />
    );
  } else {
    body = (
      <>
        <HeroCard
          hero={hero}
          info={info}
          pills={state.pills}
          elapsed={elapsed}
          busy={busy}
          now={now}
          onBreak={(usePill) => act('breakthrough', { use_pill: usePill })}
          onQi={(item) => act('qi', { item })}
        />
        <Tabs type='button' activeKey={tab} onChange={setTab} size='small'>
          <TabPane tab={t('地图')} itemKey='map'>
            <MapPanel
              info={info}
              hero={hero}
              now={now}
              busy={busy}
              selected={selected}
              onSelect={setSelected}
              onStory={(next) => setStory({ ...next, key: Date.now() })}
              onVisit={(node) => act('visit', { node })}
              onFight={async (node, team) => {
                const data = await act('fight', { node, team });
                if (data) playSound('start');
              }}
            />
          </TabPane>
          <TabPane tab={t('问心阶')} itemKey='steps'>
            <StepsPanel
              info={info}
              hero={hero}
              busy={busy}
              onClimb={async (team) => {
                const data = await act('climb', { team });
                if (data) playSound('start');
              }}
            />
          </TabPane>
          <TabPane
            tab={t('功法 {{n}}', { n: hero.cards.length })}
            itemKey='cards'
          >
            <Collection
              info={info}
              hero={hero}
              res={state.res}
              busy={busy}
              act={act}
            />
          </TabPane>
          <TabPane tab={t('洞府')} itemKey='cave'>
            <Cave
              info={info}
              hero={hero}
              res={state.res}
              rare={state.rare}
              now={now}
              busy={busy}
              act={act}
            />
          </TabPane>
          <TabPane tab={t('法宝与行囊')} itemKey='gear'>
            <Gear
              info={info}
              hero={hero}
              res={state.res}
              busy={busy}
              act={act}
            />
          </TabPane>
        </Tabs>
      </>
    );
  }

  return (
    <WendaoLayout
      loading={!state && !error}
      className='jn-page'
      side={
        <>
          {hero && (
            <SideBox
              label={t('历练战绩')}
              value={hero.stats.wins}
              note={[
                t('胜 {{wins}} · 负 {{losses}} · 突破 {{breaks}} 次', {
                  wins: hero.stats.wins,
                  losses: hero.stats.losses,
                  breaks: hero.stats.breakthroughs,
                }),
                hero.steps_open &&
                  t('问心阶 {{n}} / {{max}}', {
                    n: hero.steps,
                    max: info.steps.length,
                  }),
              ]
                .filter(Boolean)
                .join(' · ')}
            />
          )}
          <SideBox label={t('玩法说明')}>
            <RuleList
              items={[
                t(
                  '历练是宗主本人的修仙路：沿着主线一章章往下走，每章对应一个大境界',
                ),
                t(
                  '战斗用功法牌组，每场都满血开打；赢了能从三张功法里选一张收下',
                ),
                t('第一次通过节点有全部奖励；之后修为减半，资源每天只有前几次'),
                t(
                  '主角随时在闭关，离线最多结算 12 小时；境界每高一重闭关快一倍，宗门的聚灵阵也让修炼更快',
                ),
                t('第九层圆满后，用本章首领掉落的机缘突破，才能进入下一章'),
                t(
                  '重铸封印之后开放问心阶：五十级心魔一级比一级强，第一次登上每一级有修为、资源与丹药',
                ),
                t(
                  '奖励的灵石、灵草、玄铁直接进宗门库房，参悟功法也花宗门的灵石',
                ),
                t(
                  '洞府是宗主本人的修行之所：静室加快闭关，灵泉增加生命，丹室开炉炼丹，器室祭炼法宝',
                ),
                t('历练不发游戏币'),
              ]}
            />
          </SideBox>
        </>
      }
    >
      <div className='games-stage jn-stage'>
        {notice && (
          <div className='sc-notice jn-notice' role='status'>
            <div>
              {notice.lines.map((line, index) => (
                <p key={index}>{line}</p>
              ))}
              {notice.gains.length > 0 && (
                <p className='sc-muted'>{notice.gains.join(' · ')}</p>
              )}
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
        {body}
        {error && <StageMessage kind='error'>{error}</StageMessage>}
      </div>
    </WendaoLayout>
  );
};

// 历练要用秘境的功法、法宝文案与宗门的资源、境界文案。
const Journey = () => (
  <DungeonTextProvider>
    <SectTextProvider>
      <JourneyPage />
    </SectTextProvider>
  </DungeonTextProvider>
);

export default Journey;

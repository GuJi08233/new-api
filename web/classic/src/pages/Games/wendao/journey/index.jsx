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
import { useTranslation } from 'react-i18next';
import { RuleList, SideBox, StageMessage } from '../../common/SideBox';
import { gameGet, gamePostOnce } from '../../common/api';
import { playSound } from '../../common/sound';
import { celebrate } from '../../common/effects';
import { useVisiblePolling } from '../../common/useVisiblePolling';
import { DungeonTextProvider, useDungeonText } from '../../dungeon/parts';
import { SectTextProvider, useSectText } from '../../sect/parts';
import { WendaoLayout, useWendaoPart, useWendaoShell } from '../shell';
import TravelNotice from '../world/TravelNotice';
import CreateHero from './CreateHero';
import HeroCard from './HeroCard';
import MapPanel from './MapPanel';
import StepsPanel from './StepsPanel';
import TrialsPanel from './TrialsPanel';
import Collection from './Collection';
import Gear from './Gear';
import Cave from '../cave/Cave';
import DoufaPractice from '../doufa';
import BattleStage from './BattleStage';
import StoryDialog from './StoryDialog';
import { heroEventLines } from './lines';
import { encodeInputs } from '../doufa/engine';
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
  const { focus, go, refreshAgenda } = useWendaoShell();
  const focused = focus?.part === 'journey' ? focus : null;
  const [state, setState] = useState(null);
  const [receivedAt, setReceivedAt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [tab, setTab] = useState(focused?.tab || 'map');
  const [selected, setSelected] = useState(focused?.node || null);
  const [notice, setNotice] = useState(null);
  const [ended, setEnded] = useState(null);
  const [fight, setFight] = useState(null);
  // story 是正在播放的一段剧情：lines、chapter、title、doneLabel、抉择 choices 与播完后做什么(onDone，有抉择时收到所选
  // 的 key)。
  const [story, setStory] = useState(null);
  const busyRef = useRef(false);
  const [, setTick] = useState(0);

  useEffect(() => {
    const timer = setInterval(() => setTick((n) => n + 1), 1000);
    return () => clearInterval(timer);
  }, []);

  // 从顶上的目标或待办点过来：收起剧情与战报，打开要去的页签(地图上选中那个节点)。
  useEffect(() => {
    if (!focused) return;
    setStory(null);
    setEnded(null);
    if (focused.tab) setTab(focused.tab);
    if (focused.node) setSelected(focused.node);
  }, [focused]);

  const accept = useCallback((data) => {
    setState({
      hero: data.hero,
      res: data.res,
      pills: data.pills,
      rare: data.rare,
    });
    setReceivedAt(Date.now());
    // 种子与开局只由开战回应发一次；轮询或重读概况不会把本页正在打的房间抹掉。
    setFight(
      (current) =>
        data.fight ||
        (data.hero?.battle?.token === current?.token ? current : null),
    );
  }, []);

  // writes 是发出过几次操作：读取发出以后又有操作发出，这次读到的就可能比操作的回应旧，不拿它覆盖。
  const writes = useRef(0);
  const sync = useCallback(async () => {
    const at = writes.current;
    const response = await gameGet('/api/game/wendao/hero', t);
    if (writes.current !== at) return;
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
    writes.current += 1;
    setBusy(true);
    setError('');
    try {
      const response = await gamePostOnce(
        action === 'clear'
          ? '/api/game/wendao/hero/clear'
          : '/api/game/wendao/hero/action',
        action === 'clear' ? fields : { action, ...fields },
        t,
      );
      if (!response.data) {
        playSound('error');
        if (response.network || action === 'clear' || action === 'flee')
          await sync();
        setError(response.error);
        return null;
      }
      const data = response.data;
      accept(data);
      if (data.replayed)
        Toast.info(t('网络刚才断了一下，这一步上次已经做完了'));
      refreshAgenda();
      if (data.ended) {
        const won = data.ended.won;
        playSound(won ? 'win' : 'lose');
        const milestone = data.events.some(
          (ev) =>
            ev.k === 'hero_key' || (ev.k === 'hero_step' && ev.n % 10 === 0),
        );
        if (won && milestone) celebrate(1);
        setEnded({
          ...data.ended,
          events: data.events || [],
          fled: data.ended.fled || action === 'flee',
        });
      } else if (data.events?.length) {
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

  const clear = (run) =>
    act('clear', {
      inputs: encodeInputs(run.inputs),
      seed: run.seed,
      issued: run.issued,
      battle_token: run.token,
      request_id: run.requestId,
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
        choices={story.choices}
        onDone={async (choice) => {
          const next = story.onDone;
          setStory(null);
          if (next) await next(choice);
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
        key={hero.battle?.token || 'ended'}
        hero={hero}
        busy={busy}
        fight={fight}
        ended={ended}
        onClear={clear}
        onFlee={() => act('flee', { battle_token: hero.battle.token })}
        onBack={() => setEnded(null)}
        onGo={(target) => go({ part: 'journey', ...target })}
      />
    );
  } else {
    body = (
      <>
        <TravelNotice active={hero.travel} meditation={hero.meditation} />
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
              onVisit={(node, option) => act('visit', { node, option })}
              onFight={async (node, team) => {
                const data = await act('fight', { node, team });
                if (data) playSound('start');
              }}
            />
          </TabPane>
          <TabPane tab={t('试剑')} itemKey='doufa'>
            {tab === 'doufa' && <DoufaPractice hero={hero} />}
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
          <TabPane tab={t('周天试炼')} itemKey='trial'>
            <TrialsPanel
              info={info}
              hero={hero}
              now={now}
              busy={busy}
              onTrial={async (index, team) => {
                const data = await act('trial', { index, team });
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
              pills={state.pills}
              rare={state.rare}
              now={now}
              busy={busy}
              act={act}
              focus={focused}
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
                  '历练是主角本人的修仙路：沿着主线一章章往下走，每章对应一个大境界',
                ),
                t(
                  '章节、问心阶与周天试炼都用斗法，每场满血开打；走位、攻击、法术与身法决定胜负',
                ),
                t('第一次通过节点有全部奖励；之后修为减半，资源每天只有前几次'),
                t(
                  '主角平时自动闭关，游历时暂停；离线最多结算 12 小时，境界与聚灵阵提高闭关速度',
                ),
                t('第九层圆满后，用本章首领掉落的机缘突破，才能进入下一章'),
                t(
                  '重铸封印之后开放问心阶：五十级心魔一级比一级强，第一次登上每一级有修为、资源与丹药',
                ),
                t('奖励的灵石、灵草、玄铁直接进库房，参悟功法也花库房里的灵石'),
                t('已参悟的功法提高斗法伤害，不再需要编排牌组'),
                t('丹符开战时自动消耗，转成本场加成；随行弟子与灵兽也提供加成'),
                t('在世界的斗法配置里挑选法宝与法术，历练也沿用这套配置'),
                t(
                  '洞府是主角本人的修行之所：静室加快闭关，灵泉增加生命，丹室开炉炼丹，器室祭炼法宝',
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

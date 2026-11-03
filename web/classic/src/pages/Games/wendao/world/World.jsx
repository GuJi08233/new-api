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
import { Button } from '@douyinfe/semi-ui';
import { Lock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { gameGet, gamePost, gamePostOnce } from '../../common/api';
import { playSound } from '../../common/sound';
import { useVisiblePolling } from '../../common/useVisiblePolling';
import { SectTextProvider, useSectText } from '../../sect/parts';
import { WendaoLayout, useWendaoPart, useWendaoShell } from '../shell';
import { ActiveDuelNotice, ContractList } from '../duel/Contracts';
import DuelRun from '../duel/DuelRun';
import ArmsPanel from './ArmsPanel';
import TravelPanel from './TravelPanel';
import MeditationPanel from './MeditationPanel';
import { REGION_IDS, getRegionText } from '../duel/text';
import '../../sect/sect.css';
import '../doufa/doufa.css';

const API = '/api/game/wendao/duel';

const WorldPage = () => {
  const { t } = useTranslation();
  const text = useSectText();
  const { info } = useWendaoPart('world');
  const { go, refreshAgenda, focus } = useWendaoShell();
  const focused = focus?.part === 'world' ? focus : null;
  const [state, setState] = useState(null);
  const [region, setRegion] = useState(focused?.region ?? null);
  const [duel, setDuel] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [receivedAt, setReceivedAt] = useState(0);
  const [loot, setLoot] = useState(null);
  const requestSeq = useRef(0);
  const busyRef = useRef(false);
  const regions = getRegionText(t);

  const load = useCallback(async () => {
    const mine = ++requestSeq.current;
    const [world, travel] = await Promise.all([
      gameGet(`${API}/state`, t),
      gameGet('/api/game/wendao/travel', t),
    ]);
    if (mine !== requestSeq.current) return;
    if (world.error || travel.error) {
      setError(world.error || travel.error);
      return;
    }
    setState({ ...world.data, ...travel.data });
    setError('');
    setReceivedAt(Date.now());
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);
  useVisiblePolling(load, 60 * 1000, !!state && !duel && !busy);

  const book = state?.book;
  const ready = !!book;
  useEffect(() => {
    if (!focused) return;
    setRegion(
      Math.max(0, Math.min(REGION_IDS.length - 1, focused.region ?? 0)),
    );
  }, [focused]);
  useEffect(() => {
    if (!ready || !focused?.view) return;
    document
      .getElementById(`wd-world-${focused.view}`)
      ?.scrollIntoView({ block: 'start' });
  }, [ready, focused]);
  // 默认停在主角够得着的最远一个地域。
  const here = region ?? Math.max(0, (book?.open || []).lastIndexOf(true));

  const giveUp = async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    requestSeq.current += 1;
    setBusy(true);
    setError('');
    try {
      const res = await gamePostOnce(`${API}/giveup`, {}, t);
      requestSeq.current += 1;
      if (res.error) {
        await load();
        setError(res.error);
        return;
      }
      setState((prev) => ({ ...prev, ...res.data }));
      setReceivedAt(Date.now());
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const equip = async (weapons, skill) => {
    if (busyRef.current) return;
    busyRef.current = true;
    requestSeq.current += 1;
    setBusy(true);
    setError('');
    try {
      const res = await gamePost(`${API}/arms`, { weapons, skill }, t);
      requestSeq.current += 1;
      if (res.error) {
        await load();
        setError(res.error);
        return;
      }
      playSound('sparkle');
      setState((prev) => ({ ...prev, ...res.data }));
      setReceivedAt(Date.now());
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const idleAction = async (kind, fields) => {
    if (busyRef.current) return;
    busyRef.current = true;
    requestSeq.current += 1;
    setBusy(true);
    setError('');
    setLoot(null);
    try {
      const res = await gamePostOnce(
        `/api/game/wendao/${kind}/action`,
        fields,
        t,
      );
      requestSeq.current += 1;
      if (res.error) {
        await load();
        setError(res.error);
        return;
      }
      setState((prev) => ({ ...prev, ...res.data }));
      setReceivedAt(Date.now());
      setLoot(res.data.loot || null);
      playSound(
        fields.action === 'start'
          ? 'start'
          : kind === 'travel'
            ? 'coin'
            : 'sparkle',
      );
      refreshAgenda();
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  };

  const finished = (answer) => {
    setDuel(null);
    if (answer?.book) {
      requestSeq.current += 1;
      setState((prev) => ({ ...prev, ...answer }));
      setReceivedAt(Date.now());
    } else load();
    refreshAgenda();
  };

  if (!state) {
    return error ? <div className='df-error'>{error}</div> : null;
  }
  if (!book) {
    return (
      <div className='df-card'>
        <div className='df-title'>{t('世界')}</div>
        <p className='df-lead'>
          {t('先在「历练」里选定道途，再来看沧澜界的世界地图。')}
        </p>
        <div className='df-actions'>
          <Button theme='solid' onClick={() => go({ part: 'journey' })}>
            {t('去历练')}
          </Button>
        </div>
      </div>
    );
  }
  if (duel) {
    return <DuelRun kind={duel.kind} id={duel.id} onDone={finished} />;
  }

  const id = REGION_IDS[here];
  const open = book.open[here];
  const sect = info?.regions?.[here]?.sect;
  const todays = book.bounties.filter((c) => c.region === here);
  return (
    <div className='df-panel'>
      <div className='dl-regions' role='tablist' aria-label={t('地域')}>
        {REGION_IDS.map((key, i) => (
          <button
            key={key}
            type='button'
            role='tab'
            aria-selected={here === i}
            className={`dl-region ${here === i ? 'is-active' : ''} ${book.open[i] ? '' : 'is-locked'}`}
            onClick={() => setRegion(i)}
          >
            <strong>{regions[key].name}</strong>
            <small>
              {book.open[i] ? (
                t('{{n}} 张悬赏', {
                  n: book.bounties.filter((c) => c.region === i && !c.done)
                    .length,
                })
              ) : (
                <>
                  <Lock size={11} /> {text.realms[i]}
                </>
              )}
            </small>
          </button>
        ))}
      </div>
      <div id='wd-world-travel'>
        <TravelPanel
          travel={state.travel}
          region={here}
          receivedAt={receivedAt}
          blocked={!!book.active || !!state.hero?.battle}
          riftActive={state.rift_active}
          meditation={state.meditation?.active}
          busy={busy}
          loot={loot}
          onStart={(target) =>
            idleAction('travel', { action: 'start', region: target })
          }
          onFinish={(token) =>
            idleAction('travel', { action: 'finish', token })
          }
        />
      </div>
      <div id='wd-world-meditation'>
        <MeditationPanel
          meditation={state.meditation}
          region={here}
          travel={state.travel?.active}
          blocked={!!book.active || !!state.hero?.battle}
          riftActive={state.rift_active}
          busy={busy}
          onStart={(target) =>
            idleAction('meditation', { action: 'start', region: target })
          }
          onLeave={(token) =>
            idleAction('meditation', { action: 'leave', token })
          }
        />
      </div>
      <div className='df-card'>
        <div className='df-title'>{regions[id].name}</div>
        <p className='df-lead'>{regions[id].desc}</p>
        {!open && (
          <p className='df-muted'>
            {t('要{{need}}，现在{{realm}}', {
              need: text.realms[here],
              realm: text.realms[state.standing?.realm ?? 0],
            })}
          </p>
        )}
        <ul className='dl-features'>
          <li>
            <span>{t('历练主线：第 {{n}} 章', { n: here + 1 })}</span>
            <Button
              size='small'
              onClick={() => go({ part: 'journey', tab: 'map' })}
            >
              {t('去历练')}
            </Button>
          </li>
          {sect && (
            <li>
              <span>
                {t('山门：{{sect}}', { sect: text.path.sects[sect]?.name })}
              </span>
              <Button
                size='small'
                onClick={() => go({ part: 'sect', tab: 'overview' })}
              >
                {t('去身份页')}
              </Button>
            </li>
          )}
          {here === 0 && (
            <li>
              <span>{t('九幽裂隙：九幽秘境的入口')}</span>
              <Button size='small' onClick={() => go({ part: 'abyss' })}>
                {t('去秘境')}
              </Button>
            </li>
          )}
        </ul>
        <div id='wd-world-bounties' className='dl-heading'>
          {t('悬赏榜')}
        </div>
        <p className='df-muted'>
          {t(
            '悬赏榜每天换一批，每张最多挑战 {{n}} 次；打赢给宗门资源，敌人随地域与难度变强。',
            { n: info?.contract_tries ?? 2 },
          )}
        </p>
        <ActiveDuelNotice active={book.active} busy={busy} onGiveUp={giveUp} />
        {todays.length === 0 ? (
          <p className='df-muted'>
            {open ? t('今天这里没有悬赏。') : t('这里还去不了。')}
          </p>
        ) : (
          <ContractList
            contracts={todays}
            open={book.open}
            active={book.active}
            blocked={!!state.travel?.active || !!state.meditation?.active}
            busy={busy}
            maxTries={info?.contract_tries ?? 2}
            onStart={(c) => setDuel({ kind: c.kind, id: c.id })}
          />
        )}
        {error && <div className='df-error'>{error}</div>}
      </div>
      <ArmsPanel arms={book.arms} busy={busy} onEquip={equip} />
    </div>
  );
};

// 世界地图：五个地域对应五章，主角的境界到了才去得了；每个地域有历练的主线、正道宗门的山门，和每天换一批的悬赏榜。
export default function World() {
  return (
    <SectTextProvider>
      <WendaoLayout className='dg-page'>
        <WorldPage />
      </WendaoLayout>
    </SectTextProvider>
  );
}

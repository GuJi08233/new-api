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

import React from 'react';
import { Banner, Button } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';

export default function PendingReplay({ replay }) {
  const { t } = useTranslation();
  return (
    <>
      {replay.storageWarning && (
        <Banner
          type='warning'
          closeIcon={null}
          description={t(
            '浏览器无法保存游戏进度，请在本页完成结算，刷新或离开可能丢失进度',
          )}
        />
      )}
      {replay.pending.length > 0 && (
        <Banner
          type='warning'
          closeIcon={null}
          title={t('发现尚未确认结算的对局')}
          description={
            <div>
              <p>
                {t('请先提交上次对局，再开始新游戏。重复提交不会重复发奖。')}
              </p>
              {replay.pending.map((record) => (
                <Button
                  key={record.sessionId}
                  loading={replay.busy}
                  disabled={replay.busy}
                  onClick={() => replay.retry(record)}
                >
                  {t('恢复提交对局 #{{id}}', { id: record.sessionId })}
                </Button>
              ))}
            </div>
          }
        />
      )}
      {replay.message && <p role='status'>{replay.message}</p>}
    </>
  );
}

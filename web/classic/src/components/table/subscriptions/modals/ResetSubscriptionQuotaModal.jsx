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

import React, { useEffect, useState } from 'react';
import { Modal, Space, Switch, Typography } from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

const ResetSubscriptionQuotaModal = ({
  visible,
  planRecord,
  handleClose,
  refresh,
  t,
}) => {
  const [advanceResetTime, setAdvanceResetTime] = useState(true);
  const [resetting, setResetting] = useState(false);
  const plan = planRecord?.plan;

  useEffect(() => {
    if (visible) setAdvanceResetTime(true);
  }, [visible]);

  const resetSubscriptions = async () => {
    if (!plan?.id) return;
    setResetting(true);
    try {
      const res = await API.post(
        `/api/subscription/admin/plans/${plan.id}/subscriptions/reset`,
        { advance_reset_time: advanceResetTime },
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('重置订阅额度失败'));
        return;
      }
      showSuccess(
        t('已重置 {{count}} 个有效订阅的额度', {
          count: data?.reset_count || 0,
        }),
      );
      await refresh?.();
      handleClose();
    } catch (error) {
      showError(error?.message || t('重置订阅额度失败'));
    } finally {
      setResetting(false);
    }
  };

  return (
    <Modal
      title={t('重置订阅额度')}
      visible={visible}
      onCancel={handleClose}
      onOk={resetSubscriptions}
      okText={t('确认重置')}
      cancelText={t('取消')}
      confirmLoading={resetting}
      centered
    >
      <Space vertical align='start' style={{ width: '100%' }} spacing={16}>
        <Text>
          {t('确定重置套餐「{{plan}}」下全部有效订阅的额度吗？', {
            plan: plan?.title || (plan?.id ? `#${plan.id}` : '-'),
          })}
        </Text>
        <Space>
          <Switch checked={advanceResetTime} onChange={setAdvanceResetTime} />
          <div>
            <Text>{t('推进下次重置时间')}</Text>
            <Text type='tertiary' size='small' style={{ display: 'block' }}>
              {t('开启后会从本次手动重置时间重新计算下一次自动重置时间')}
            </Text>
          </div>
        </Space>
      </Space>
    </Modal>
  );
};

export default ResetSubscriptionQuotaModal;

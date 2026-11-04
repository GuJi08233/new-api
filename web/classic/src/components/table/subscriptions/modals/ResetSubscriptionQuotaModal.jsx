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

import React, { useState } from 'react';
import { Modal, Space, Typography } from '@douyinfe/semi-ui';
import { API, showError, showSuccess } from '../../../../helpers';

const { Text } = Typography;

const ResetSubscriptionQuotaModal = ({
  visible,
  planRecord,
  handleClose,
  refresh,
  t,
}) => {
  const [resetting, setResetting] = useState(false);
  const plan = planRecord?.plan;

  const resetSubscriptions = async () => {
    if (!plan?.id) return;
    setResetting(true);
    try {
      const res = await API.post(
        `/api/subscription/admin/plans/${plan.id}/subscriptions/reset`,
        { advance_reset_time: false },
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
        <Text type='tertiary' size='small'>
          {t(
            '将每档已用额度清零，恢复可用额度；套餐限额、订阅有效期和钱包余额保持不变。',
          )}
        </Text>
        <Text type='tertiary' size='small'>
          {t('仅清零已用额度，保留各档位原有的自动重置时间。')}
        </Text>
      </Space>
    </Modal>
  );
};

export default ResetSubscriptionQuotaModal;

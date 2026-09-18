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
import { SideSheet, Typography, Button, Divider } from '@douyinfe/semi-ui';
import { IconClose } from '@douyinfe/semi-icons';

import { calculateModelPrice } from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import ModelHeader from './components/ModelHeader';
import ModelBasicInfo from './components/ModelBasicInfo';
import ModelEndpoints from './components/ModelEndpoints';
import ModelPricingTable from './components/ModelPricingTable';
import DynamicPricingBreakdown from './components/DynamicPricingBreakdown';

const { Text } = Typography;

const ModelDetailSideSheet = ({
  visible,
  onClose,
  modelData,
  groupRatio,
  selectedGroup,
  currency,
  siteDisplayType,
  tokenUnit,
  displayPrice,
  showRatio,
  usableGroup,
  vendorsMap,
  endpointMap,
  autoGroups,
  t,
  groupPricing = null, // 新增：分组定价数据
}) => {
  const isMobile = useIsMobile();
  // 与卡片视图共用同一份分组选择结果，两处显示的单价才会是同一个数
  const priceData = modelData
    ? calculateModelPrice({
        record: modelData,
        selectedGroup,
        groupRatio,
        tokenUnit,
        displayPrice,
        currency,
        quotaDisplayType: siteDisplayType,
        groupPricing,
      })
    : null;

  return (
    <SideSheet
      placement='right'
      title={
        <ModelHeader modelData={modelData} vendorsMap={vendorsMap} t={t} />
      }
      bodyStyle={{
        padding: '0',
        display: 'flex',
        flexDirection: 'column',
        borderBottom: '1px solid var(--semi-color-border)',
      }}
      visible={visible}
      width={isMobile ? '100%' : 600}
      closeIcon={
        <Button
          className='semi-button-tertiary semi-button-size-small semi-button-borderless'
          type='button'
          icon={<IconClose />}
          onClick={onClose}
        />
      }
      onCancel={onClose}
    >
      <div style={{ paddingTop: 16, paddingBottom: 16 }}>
        {!modelData && (
          <div className='flex justify-center items-center py-10'>
            <Text type='secondary'>{t('加载中...')}</Text>
          </div>
        )}
        {modelData && (
          <>
            <div style={{ padding: '0 24px' }}>
              <ModelBasicInfo
                modelData={modelData}
                vendorsMap={vendorsMap}
                t={t}
              />
            </div>
            <Divider margin={16} />
            <div style={{ padding: '0 24px' }}>
              <ModelEndpoints
                modelData={modelData}
                endpointMap={endpointMap}
                t={t}
              />
            </div>
            {priceData?.isDynamicPricing && priceData.billingExpr && (
              <>
                <Divider margin={16} />
                <div style={{ padding: '0 24px' }}>
                  <DynamicPricingBreakdown
                    billingExpr={priceData.billingExpr}
                    t={t}
                    groupRatio={priceData.usedGroupRatio ?? 1}
                    groupName={priceData.usedGroup || ''}
                    tokenUnit={tokenUnit}
                    displayPrice={displayPrice}
                  />
                </div>
              </>
            )}
            <Divider margin={16} />
            <div style={{ padding: '0 24px' }}>
              <ModelPricingTable
                modelData={modelData}
                groupRatio={groupRatio}
                currency={currency}
                siteDisplayType={siteDisplayType}
                tokenUnit={tokenUnit}
                displayPrice={displayPrice}
                showRatio={showRatio}
                usableGroup={usableGroup}
                autoGroups={autoGroups}
                t={t}
                groupPricing={groupPricing}
                breakdownExpr={priceData?.billingExpr || ''}
              />
            </div>
            <Divider margin={16} />
          </>
        )}
      </div>
    </SideSheet>
  );
};

export default ModelDetailSideSheet;

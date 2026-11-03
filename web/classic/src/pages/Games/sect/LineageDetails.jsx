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

// 师承记录永久保留，授业是否生效则采用服务端当前自然修为的结果。
export default function LineageDetails({ disciple, disciples, name, t }) {
  const teacher = disciples.find((d) => d.id === disciple.teacher_id);
  const teacherName = disciple.teacher_name || teacher?.name;
  return (
    <div className='sc-lineage'>
      <span>{t('第 {{n}} 代弟子', { n: disciple.generation || 1 })}</span>
      {disciple.teacher_id > 0 && (
        <>
          <span>
            {teacherName
              ? t('师承：{{name}}', { name: name(teacherName) })
              : t('师承记录保留')}
          </span>
          <span className='sc-muted'>
            {disciple.teaching_pct > 0
              ? t('授业生效，自然修为 +{{pct}}%', {
                  pct: disciple.teaching_pct,
                })
              : teacher
                ? t('授业暂未生效')
                : t('师长已离宗，师承记录保留')}
          </span>
        </>
      )}
    </div>
  );
}

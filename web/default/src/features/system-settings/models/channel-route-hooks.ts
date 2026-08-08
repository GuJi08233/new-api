/*
Copyright (C) 2023-2026 QuantumNous

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
import { useCallback, useEffect, useState } from 'react'

export function useChannelNameMap() {
  const [channelNames, setChannelNames] = useState<Map<number, string>>(
    new Map()
  )

  useEffect(() => {
    fetch('/api/channel/?p=0&page_size=500&id_sort=true')
      .then((response) => response.json())
      .then((data) => {
        if (!data.success || !data.data?.items) return
        const nextChannelNames = new Map<number, string>()
        for (const channel of data.data.items) {
          nextChannelNames.set(channel.id, channel.name)
        }
        setChannelNames(nextChannelNames)
      })
      .catch(() => {})
  }, [])

  return useCallback(
    (id: number) => channelNames.get(id) || `#${id}`,
    [channelNames]
  )
}

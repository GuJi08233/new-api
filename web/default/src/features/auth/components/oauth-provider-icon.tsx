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
import type { ReactNode } from 'react'

import { IconDiscord } from '@/assets/brand-icons/icon-discord'
import { IconFacebook } from '@/assets/brand-icons/icon-facebook'
import { IconGithub } from '@/assets/brand-icons/icon-github'
import { IconGitlab } from '@/assets/brand-icons/icon-gitlab'
import { IconNotion } from '@/assets/brand-icons/icon-notion'
import { IconSlack } from '@/assets/brand-icons/icon-slack'
import { IconTelegram } from '@/assets/brand-icons/icon-telegram'
import { IconWeChat } from '@/assets/brand-icons/icon-wechat'
import { cn } from '@/lib/utils'

type OAuthProviderIconProps = {
  icon?: string
  className?: string
}

export function OAuthProviderIcon(props: OAuthProviderIconProps) {
  const raw = String(props.icon || '').trim()
  const normalized = raw
    .toLowerCase()
    .replace(/^ri:/, '')
    .replace(/^react-icons:/, '')
    .replace(/^si:/, '')

  let content: ReactNode

  if (/^https?:\/\//i.test(raw)) {
    content = (
      <img
        src={raw}
        alt=''
        className='size-full rounded-sm object-contain'
        referrerPolicy='no-referrer'
      />
    )
  } else {
    switch (normalized) {
      case 'discord':
        content = <IconDiscord className='size-full' />
        break
      case 'facebook':
        content = <IconFacebook className='size-full' />
        break
      case 'github':
        content = <IconGithub className='size-full' />
        break
      case 'gitlab':
        content = <IconGitlab className='size-full' />
        break
      case 'notion':
        content = <IconNotion className='size-full' />
        break
      case 'slack':
        content = <IconSlack className='size-full' />
        break
      case 'telegram':
        content = <IconTelegram className='size-full' />
        break
      case 'wechat':
        content = <IconWeChat className='size-full' />
        break
      default:
        content = (
          <span className='flex size-full items-center justify-center text-xs font-medium'>
            {raw.length <= 4 ? raw : raw.charAt(0).toUpperCase()}
          </span>
        )
    }
  }

  return (
    <span
      aria-hidden='true'
      className={cn('inline-flex size-4 shrink-0', props.className)}
    >
      {content}
    </span>
  )
}

import type { LiveAnchorStyle } from './types'
export type AnchorAvatarType = NonNullable<LiveAnchorStyle['avatar_type']>
export const anchorAvatarOptions: { value: AnchorAvatarType; label: string }[] = [
  { value: 'male', label: '男' }, { value: 'female', label: '女' },
  { value: 'middle_aged_male', label: '中年男' }, { value: 'middle_aged_female', label: '中年女' },
]
export function anchorAvatarType(value?: string): AnchorAvatarType {
  return anchorAvatarOptions.some(option => option.value === value) ? value as AnchorAvatarType : 'female'
}
export function anchorAvatarURL(value?: string): string {
  return '/assets/anchor-avatars/' + anchorAvatarType(value) + '-v1.png'
}
export function anchorPresenterAvatar(style: { avatar_url?: string; avatar_type?: string }): string {
  return /^data:image\/(jpeg|png);base64,[A-Za-z0-9+/=]+$/.test(style.avatar_url || '') ? style.avatar_url! : anchorAvatarURL(style.avatar_type)
}

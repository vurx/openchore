import { describe, it, expect, beforeAll } from 'vitest';
import i18n from './index';

beforeAll(async () => {
  if (!i18n.isInitialized) {
    await i18n.init();
  }
});

describe('i18n configuration', () => {
  it('resolves a known key to its English string', () => {
    expect(i18n.t('common.back')).toBe('Back');
  });

  it('falls back to English for an unsupported language', async () => {
    await i18n.changeLanguage('zz-ZZ');
    expect(i18n.resolvedLanguage).toBe('en');
    expect(i18n.t('common.save')).toBe('Save');
  });

  it('interpolates variables', () => {
    expect(i18n.t('test.greeting', { name: 'Sam' })).toBe('Hi, Sam');
  });

  it('selects singular vs plural', () => {
    expect(i18n.t('test.points', { count: 1 })).toBe('1 point');
    expect(i18n.t('test.points', { count: 3 })).toBe('3 points');
  });

  it('switches to Chinese and resolves translations correctly', async () => {
    await i18n.changeLanguage('zh-CN');
    expect(i18n.resolvedLanguage).toBe('zh');
    expect(i18n.t('common.back')).toBe('返回');
    await i18n.changeLanguage('zh');
    expect(i18n.resolvedLanguage).toBe('zh');
    expect(i18n.t('common.back')).toBe('返回');
    expect(i18n.t('common.save')).toBe('保存');
    expect(i18n.t('test.greeting', { name: '小明' })).toBe('你好，小明');
    expect(i18n.t('test.points', { count: 5 })).toBe('5 积分');
    // Switch back to fallback
    await i18n.changeLanguage('en');
  });
});

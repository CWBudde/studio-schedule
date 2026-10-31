import { renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { describe, expect, it } from 'vitest';
import { programme } from './programme';
import type { DayHeadingProps, Theme } from './slots';
import { ThemeContext } from './themeContext';
import { useSlot } from './useSlot';

function FakeDayHeading(_props: DayHeadingProps) {
  return null;
}

const fake: Theme = { id: 'werkstatt', slots: { DayHeading: FakeDayHeading } };

function inFake({ children }: { children: ReactNode }) {
  return <ThemeContext.Provider value={fake}>{children}</ThemeContext.Provider>;
}

describe('useSlot', () => {
  it("gives Programme's slots without a theme", () => {
    const { result } = renderHook(() => [useSlot('DayHeading'), useSlot('LessonCard')]);

    expect(result.current[0]).toBe(programme.slots.DayHeading);
    expect(result.current[1]).toBe(programme.slots.LessonCard);
  });

  it("gives the theme's slot, and Programme's where the theme has none", () => {
    const { result } = renderHook(() => [useSlot('DayHeading'), useSlot('LessonCard')], {
      wrapper: inFake,
    });

    expect(result.current[0]).toBe(FakeDayHeading);
    expect(result.current[1]).toBe(programme.slots.LessonCard);
  });
});

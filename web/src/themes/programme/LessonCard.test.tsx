import { screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import { lessonBallet, meOffice, mockApiGet } from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { LessonCard } from './LessonCard';

/** Friday 2026-10-23, before the lesson. */
const now = Date.parse('2026-10-23T10:00:00Z');

/** The ballet slot rented out to an outside group (ADR 0007, amendment), as staff see it. */
const rental: ScheduleLesson = {
  ...lessonBallet,
  classId: undefined,
  levelFrom: undefined,
  levelTo: undefined,
  kind: 'rental',
  visibility: 'hidden',
  instructor: undefined,
  accompanist: undefined,
  renter: 'Copperleaf Theater Group',
  capacity: 0,
  freePlaces: 0,
};

describe('Programme LessonCard', () => {
  it('marks a room rental with its renter, without staff or free places', () => {
    mockApiGet({ '/api/v1/auth/me': { body: meOffice } });
    renderWithQuery(
      <LessonCard
        lesson={rental}
        timeZone="Europe/Berlin"
        now={now}
        past={false}
        quickAction={{ kind: 'none' }}
        onOpen={vi.fn()}
      />,
    );

    const button = screen.getByRole('button', {
      name: /Room rental, Copperleaf Theater Group, Hidden – staff only$/,
    });
    const card = within(button);
    expect(card.getByText('Room rental')).toBeInTheDocument();
    expect(card.getByText('Copperleaf Theater Group')).toBeInTheDocument();
    expect(card.getByText('Rented by:')).toHaveClass('sr-only');
    expect(card.queryByText('Instructor:')).not.toBeInTheDocument();
    expect(card.queryByText('Full')).not.toBeInTheDocument();
    expect(card.queryByText(/free/)).not.toBeInTheDocument();
  });

  it('titles a room rental without a course type "Room rental", once', () => {
    mockApiGet({ '/api/v1/auth/me': { body: meOffice } });
    renderWithQuery(
      <LessonCard
        lesson={{ ...rental, courseType: undefined }}
        timeZone="Europe/Berlin"
        now={now}
        past={false}
        quickAction={{ kind: 'none' }}
        onOpen={vi.fn()}
      />,
    );

    // Neither the card nor its name repeats the kind as a badge.
    const button = screen.getByRole('button', {
      name: 'Room rental, Monday, October 26, 18:00–19:30, Copperleaf Theater Group, Hidden – staff only',
    });
    expect(within(button).getAllByText('Room rental')).toHaveLength(1);
  });
});

import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { ids } from '@/test/mockApi';
import { renderAtSchedule } from '@/test/utils';
import { useScheduleSearch } from './useScheduleSearch';

function Probe() {
  const { search, update } = useScheduleSearch();
  return (
    <>
      <output>{JSON.stringify(search)}</output>
      <button onClick={() => void update({ view: 'week', level: undefined })}>week</button>
    </>
  );
}

describe('useScheduleSearch', () => {
  it('reads valid params, drops invalid ones and updates the URL', async () => {
    const { router } = renderAtSchedule(<Probe />, {
      level: ids.levelBeginner,
      location: ids.locationNorth,
      from: 'yesterday' as never,
    });

    expect(await screen.findByRole('status')).toHaveTextContent(
      JSON.stringify({ level: ids.levelBeginner, location: ids.locationNorth }),
    );

    await userEvent.click(screen.getByRole('button', { name: 'week' }));

    expect(router.state.location.search).toEqual({ view: 'week', location: ids.locationNorth });
  });

  it("drops an old bookmark's fixed level", async () => {
    // Before levels were the school's records, ?level= named one of a fixed set.
    renderAtSchedule(<Probe />, { level: 'beginner', courseType: ids.ballet });

    expect(await screen.findByRole('status')).toHaveTextContent(
      JSON.stringify({ courseType: ids.ballet }),
    );
  });
});

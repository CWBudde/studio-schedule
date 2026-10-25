package postgres

import (
	"reflect"
	"testing"
	"time"

	"github.com/cwbudde/studio-schedule/internal/clock"
	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/seed"
	"github.com/cwbudde/studio-schedule/internal/service"
	"github.com/cwbudde/studio-schedule/internal/store"
	"github.com/cwbudde/studio-schedule/internal/store/memory"
)

// TestStatisticsMatchMemory computes the statistics of the demo with both
// stores: they must agree, figure by figure. The figures themselves are
// checked against the demo data set in package service.
func TestStatisticsMatchMemory(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pg, mem := newTestStore(t), memory.New()

	for _, st := range []store.Store{pg, mem} {
		if err := seed.LoadDemo(ctx, st); err != nil {
			t.Fatal(err)
		}
	}

	loc, err := time.LoadLocation(seed.DemoSettings().Timezone)
	if err != nil {
		t.Fatal(err)
	}

	clk := clock.NewFixed(seed.FixedDemoAnchor.At(domain.TimeOfDay{Hour: 22}, loc))

	for _, q := range []service.StatisticsQuery{
		{Range: service.StatisticsLast4Weeks},
		{Range: service.StatisticsTerm},
		{Range: service.StatisticsCustom, From: seed.FixedDemoAnchor.AddDays(-30), To: seed.FixedDemoAnchor.AddDays(60)},
	} {
		want, err := service.NewStatisticsService(mem, clk).Statistics(ctx, q)
		if err != nil {
			t.Fatal(err)
		}

		got, err := service.NewStatisticsService(pg, clk).Statistics(ctx, q)
		if err != nil {
			t.Fatal(err)
		}

		if want.Totals.Bookings == 0 || want.Totals.Attended == 0 || want.Totals.LateCancellations == 0 ||
			want.Totals.Guests == 0 || want.Totals.PassesSold == 0 {
			t.Errorf("%+v: the demo lacks figures: %+v", q, want.Totals)
		}

		if !reflect.DeepEqual(got, want) {
			t.Errorf("%+v: PostgreSQL\n%+v\nmemory\n%+v", q, got, want)
		}
	}
}

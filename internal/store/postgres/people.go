package postgres

import (
	"context"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store/postgres/internal/sqlc"
)

// ---------------------------------------------------------------- settings

type settingsRepo struct{ q *sqlc.Queries }

func (r settingsRepo) Get(ctx context.Context) (domain.Settings, error) {
	row, err := r.q.GetSettings(ctx)
	if err != nil {
		return domain.Settings{}, notFound("settings", err)
	}

	return domain.Settings{
		SchoolName:         row.SchoolName,
		Timezone:           row.Timezone,
		DefaultLocale:      row.DefaultLocale,
		Currency:           row.Currency,
		ReceiptPrefix:      row.ReceiptPrefix,
		ReleaseWeekday:     time.Weekday(row.ReleaseWeekday),
		ReleaseTime:        fromPgTimeOfDay(row.ReleaseTime),
		ReleaseWeeksAhead:  int(row.ReleaseWeeksAhead),
		BookingCutoff:      fromPgInterval(row.BookingCutoff),
		CancellationCutoff: fromPgInterval(row.CancellationCutoff),
		DefaultVisibleDays: int(row.DefaultVisibleDays),
		LegalNotice:        row.LegalNotice.String,
	}, nil
}

func (r settingsRepo) Put(ctx context.Context, s domain.Settings) error {
	day, err := weekday(s.ReleaseWeekday)
	if err != nil {
		return err
	}

	weeksAhead, err := int4(s.ReleaseWeeksAhead)
	if err != nil {
		return err
	}

	visibleDays, err := int4(s.DefaultVisibleDays)
	if err != nil {
		return err
	}

	return mapErr(r.q.PutSettings(ctx, sqlc.PutSettingsParams{
		SchoolName:         s.SchoolName,
		Timezone:           s.Timezone,
		DefaultLocale:      s.DefaultLocale,
		Currency:           s.Currency,
		ReceiptPrefix:      s.ReceiptPrefix,
		ReleaseWeekday:     day,
		ReleaseTime:        pgTimeOfDay(s.ReleaseTime),
		ReleaseWeeksAhead:  weeksAhead,
		BookingCutoff:      pgInterval(s.BookingCutoff),
		CancellationCutoff: pgInterval(s.CancellationCutoff),
		DefaultVisibleDays: visibleDays,
		LegalNotice:        pgText(s.LegalNotice),
	}))
}

// ---------------------------------------------------------------- persons

type personRepo struct{ q *sqlc.Queries }

func toPerson(row sqlc.Person) domain.Person {
	return domain.Person{
		ID:         fromPgID[domain.PersonID](row.ID),
		GivenName:  row.GivenName,
		FamilyName: row.FamilyName,
		Email:      row.Email.String,
		Phone:      row.Phone.String,
		BirthDate:  fromPgDate(row.BirthDate),
		Notes:      row.Notes.String,
	}
}

func toPersons(rows []sqlc.Person, err error) ([]domain.Person, error) {
	return convertRows(rows, err, toPerson)
}

func (r personRepo) Get(ctx context.Context, id domain.PersonID) (domain.Person, error) {
	row, err := r.q.GetPerson(ctx, pgID(id))
	if err != nil {
		return domain.Person{}, notFound("person", err)
	}

	return toPerson(row), nil
}

func (r personRepo) Create(ctx context.Context, p domain.Person) error {
	return mapErr(r.q.CreatePerson(ctx, sqlc.CreatePersonParams{
		ID:         pgID(p.ID),
		GivenName:  p.GivenName,
		FamilyName: p.FamilyName,
		Email:      pgText(p.Email),
		Phone:      pgText(p.Phone),
		BirthDate:  pgDate(p.BirthDate),
		Notes:      pgText(p.Notes),
	}))
}

func (r personRepo) Update(ctx context.Context, p domain.Person) error {
	n, err := r.q.UpdatePerson(ctx, sqlc.UpdatePersonParams{
		ID:         pgID(p.ID),
		GivenName:  p.GivenName,
		FamilyName: p.FamilyName,
		Email:      pgText(p.Email),
		Phone:      pgText(p.Phone),
		BirthDate:  pgDate(p.BirthDate),
		Notes:      pgText(p.Notes),
	})

	return affected("person", n, err)
}

func (r personRepo) Delete(ctx context.Context, id domain.PersonID) error {
	n, err := r.q.DeletePerson(ctx, pgID(id))

	return affected("person", n, err)
}

func (r personRepo) List(ctx context.Context) ([]domain.Person, error) {
	return toPersons(r.q.ListPersons(ctx))
}

func (r personRepo) AddGuardian(ctx context.Context, g domain.Guardian) error {
	return mapErr(r.q.AddGuardian(ctx, sqlc.AddGuardianParams{
		GuardianID:  pgID(g.GuardianID),
		DependentID: pgID(g.DependentID),
	}))
}

func (r personRepo) RemoveGuardian(ctx context.Context, g domain.Guardian) error {
	n, err := r.q.RemoveGuardian(ctx, sqlc.RemoveGuardianParams{
		GuardianID:  pgID(g.GuardianID),
		DependentID: pgID(g.DependentID),
	})

	return affected("guardian link", n, err)
}

func (r personRepo) ListDependents(ctx context.Context, guardian domain.PersonID) ([]domain.Person, error) {
	return toPersons(r.q.ListDependents(ctx, pgID(guardian)))
}

func (r personRepo) ListGuardians(ctx context.Context, dependent domain.PersonID) ([]domain.Person, error) {
	return toPersons(r.q.ListGuardians(ctx, pgID(dependent)))
}

// ---------------------------------------------------------------- accounts

type accountRepo struct{ q *sqlc.Queries }

func toAccount(row sqlc.Account) domain.Account {
	return domain.Account{
		ID:          fromPgID[domain.AccountID](row.ID),
		PersonID:    fromPgID[domain.PersonID](row.PersonID),
		Email:       row.Email,
		Role:        domain.Role(row.Role),
		VisibleDays: int(row.VisibleDays.Int32),
		ConfirmedAt: fromPgInstant(row.ConfirmedAt),
		DisabledAt:  fromPgInstant(row.DisabledAt),
	}
}

func (r accountRepo) Get(ctx context.Context, id domain.AccountID) (domain.Account, error) {
	row, err := r.q.GetAccount(ctx, pgID(id))
	if err != nil {
		return domain.Account{}, notFound("account", err)
	}

	return toAccount(row), nil
}

func (r accountRepo) GetByEmail(ctx context.Context, email string) (domain.Account, error) {
	row, err := r.q.GetAccountByEmail(ctx, email)
	if err != nil {
		return domain.Account{}, notFound("account", err)
	}

	return toAccount(row), nil
}

func (r accountRepo) Create(ctx context.Context, a domain.Account) error {
	visibleDays, err := int4Opt(a.VisibleDays)
	if err != nil {
		return err
	}

	return mapErr(r.q.CreateAccount(ctx, sqlc.CreateAccountParams{
		ID:          pgID(a.ID),
		PersonID:    pgID(a.PersonID),
		Email:       a.Email,
		Role:        string(a.Role),
		VisibleDays: visibleDays,
		ConfirmedAt: pgInstant(a.ConfirmedAt),
		DisabledAt:  pgInstant(a.DisabledAt),
	}))
}

func (r accountRepo) Update(ctx context.Context, a domain.Account) error {
	visibleDays, err := int4Opt(a.VisibleDays)
	if err != nil {
		return err
	}

	n, err := r.q.UpdateAccount(ctx, sqlc.UpdateAccountParams{
		ID:          pgID(a.ID),
		PersonID:    pgID(a.PersonID),
		Email:       a.Email,
		Role:        string(a.Role),
		VisibleDays: visibleDays,
		ConfirmedAt: pgInstant(a.ConfirmedAt),
		DisabledAt:  pgInstant(a.DisabledAt),
	})

	return affected("account", n, err)
}

func (r accountRepo) List(ctx context.Context) ([]domain.Account, error) {
	rows, err := r.q.ListAccounts(ctx)

	return convertRows(rows, err, toAccount)
}

func (r accountRepo) MaxVisibleDays(ctx context.Context) (int, error) {
	n, err := r.q.MaxAccountVisibleDays(ctx)

	return int(n), mapErr(err)
}

// convertRows maps the rows of a list query, or its error.
func convertRows[R, E any](rows []R, err error, convert func(R) E) ([]E, error) {
	if err != nil {
		return nil, mapErr(err)
	}

	out := make([]E, len(rows))
	for i, row := range rows {
		out[i] = convert(row)
	}

	return out, nil
}

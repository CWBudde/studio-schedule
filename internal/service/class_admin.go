package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// The office maintains the weekly classes of the terms (ADR 0007). A class
// edit moves its future generated lessons along in the same transaction;
// see syncLessons. The HTTP layer admits only office and admin accounts to
// these methods.

// ClassData are the fields the office edits on a class. AccompanistID is
// zero for none. The term is fixed when the class is created.
type ClassData struct {
	CourseTypeID  domain.CourseTypeID
	RoomID        domain.RoomID
	InstructorID  domain.StaffID
	AccompanistID domain.StaffID
	Level         domain.Level
	Weekday       time.Weekday
	StartTime     domain.TimeOfDay
	Duration      time.Duration
	Capacity      int
	Retired       bool
}

// AllClasses returns the classes of a term with their staff like Classes,
// retired ones included. An unknown term is domain.ErrNotFound.
func (s *TimetableService) AllClasses(ctx context.Context, term domain.TermID) ([]TimetableClass, error) {
	return s.classes(ctx, term, true)
}

// CreateClass adds a weekly class to a term and generates its lessons of
// the rolling horizon that have not started yet. An unknown term is
// domain.ErrNotFound, an unknown course type, room or staff member a
// validation error on the field that names it.
func (s *TimetableService) CreateClass(ctx context.Context, term domain.TermID, d ClassData) (TimetableClass, error) {
	c := d.class(ids.New[domain.ClassID](s.ids), term)

	var tc TimetableClass

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// The term's lock keeps a change of its dates out until the class
		// and its lessons are in place.
		t, err := tx.Terms().Lock(ctx, term)
		if err != nil {
			return err
		}

		if err := c.Validate(); err != nil {
			return err
		}

		if tc, err = classReferences(ctx, tx, c); err != nil {
			return err
		}

		if err := tx.Classes().Create(ctx, c); err != nil {
			return err
		}

		return s.syncLessons(ctx, tx, t, domain.Class{}, c)
	})
	if err != nil {
		return TimetableClass{}, fmt.Errorf("create class: %w", err)
	}

	return tc, nil
}

// UpdateClass replaces a class's fields except its term. The future lessons
// that still are what the class generated follow the edit (syncLessons); a
// capacity below the active bookings of one of them is a validation error
// on capacity.
func (s *TimetableService) UpdateClass(ctx context.Context, id domain.ClassID, d ClassData) (TimetableClass, error) {
	var tc TimetableClass

	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		// An unknown class is a 404 before anything about the fields. The
		// locks, the term's first (TermRepo.Lock), keep lesson generation
		// and a change of the term's dates out until the edit is done.
		cur, err := tx.Classes().Get(ctx, id)
		if err != nil {
			return err
		}

		t, err := tx.Terms().Lock(ctx, cur.TermID)
		if err != nil {
			return fmt.Errorf("term of class %s: %w", id, err)
		}

		old, err := tx.Classes().Lock(ctx, id)
		if err != nil {
			return err
		}

		c := d.class(id, old.TermID)
		if err := c.Validate(); err != nil {
			return err
		}

		if tc, err = classReferences(ctx, tx, c); err != nil {
			return err
		}

		if err := tx.Classes().Update(ctx, c); err != nil {
			return err
		}

		return s.syncLessons(ctx, tx, t, old, c)
	})
	if err != nil {
		return TimetableClass{}, fmt.Errorf("update class %s: %w", id, err)
	}

	return tc, nil
}

// DeleteClass deletes a class with its lessons while none of them has
// started or been booked and the class has no members. Otherwise it is
// domain.ErrInUse: such a class is retired instead (ADR 0007).
func (s *TimetableService) DeleteClass(ctx context.Context, id domain.ClassID) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		if _, err := tx.Classes().Lock(ctx, id); err != nil {
			return err
		}

		members, err := tx.Classes().ListMembers(ctx, id)
		if err != nil {
			return err
		}

		if len(members) > 0 {
			return fmt.Errorf("%w: the class has members", domain.ErrInUse)
		}

		lessons, err := tx.Lessons().ListByClass(ctx, id, domain.Date{})
		if err != nil {
			return err
		}

		now := s.clock.Now()
		for _, l := range lessons {
			booked, err := hasBookings(ctx, tx, l.ID)
			if err != nil {
				return err
			}

			if booked || !l.StartsAt.After(now) {
				return fmt.Errorf("%w: lesson %s has started or has bookings", domain.ErrInUse, l.ID)
			}
		}

		for _, l := range lessons {
			if err := tx.Lessons().Delete(ctx, l.ID); err != nil {
				return err
			}
		}

		return tx.Classes().Delete(ctx, id)
	})
	// A booking or member added in the meantime makes a delete fail on its
	// reference.
	if errors.Is(err, domain.ErrConflict) && !errors.Is(err, domain.ErrInUse) {
		err = fmt.Errorf("%w (%w)", domain.ErrInUse, err)
	}

	if err != nil {
		return fmt.Errorf("delete class %s: %w", id, err)
	}

	return nil
}

// Unknown references of a class, reported on the fields that name them.
var (
	errUnknownCourseType  error = &domain.FieldError{Field: "courseTypeId", Msg: "course type does not exist"}
	errUnknownRoom        error = &domain.FieldError{Field: "roomId", Msg: "room does not exist"}
	errUnknownInstructor  error = &domain.FieldError{Field: "instructorId", Msg: "instructor does not exist"}
	errUnknownAccompanist error = &domain.FieldError{Field: "accompanistId", Msg: "accompanist does not exist"}
)

// classReferences checks that the course type, room and staff of c exist
// and returns c with its staff.
func classReferences(ctx context.Context, tx store.Tx, c domain.Class) (TimetableClass, error) {
	instructor, accompanist, err := checkReferences(ctx, tx, references{
		CourseTypeID: c.CourseTypeID, RoomID: c.RoomID, InstructorID: c.InstructorID, AccompanistID: c.AccompanistID,
	})
	if err != nil {
		return TimetableClass{}, err
	}

	return TimetableClass{Class: c, Instructor: instructor, Accompanist: accompanist}, nil
}

// references are what a class or a lesson refers to; AccompanistID is
// zero for none.
type references struct {
	CourseTypeID  domain.CourseTypeID
	RoomID        domain.RoomID
	InstructorID  domain.StaffID
	AccompanistID domain.StaffID
}

// checkReferences checks that the course type, room and staff of r exist,
// reporting a missing one on the field that names it, and returns the
// staff (a zero accompanist for none).
func checkReferences(ctx context.Context, tx store.Tx, r references) (instructor, accompanist domain.Staff, err error) {
	known := func(err, unknown error) error {
		if errors.Is(err, domain.ErrNotFound) {
			return unknown
		}

		return err
	}

	if _, err := tx.CourseTypes().Get(ctx, r.CourseTypeID); err != nil {
		return domain.Staff{}, domain.Staff{}, known(err, errUnknownCourseType)
	}

	if _, err := tx.Rooms().Get(ctx, r.RoomID); err != nil {
		return domain.Staff{}, domain.Staff{}, known(err, errUnknownRoom)
	}

	if instructor, err = tx.Staff().Get(ctx, r.InstructorID); err != nil {
		return domain.Staff{}, domain.Staff{}, known(err, errUnknownInstructor)
	}

	if !r.AccompanistID.IsZero() {
		if accompanist, err = tx.Staff().Get(ctx, r.AccompanistID); err != nil {
			return domain.Staff{}, domain.Staff{}, known(err, errUnknownAccompanist)
		}
	}

	return instructor, accompanist, nil
}

// hasBookings reports whether bookings refer to the lesson, canceled ones
// included: they keep it from being deleted.
func hasBookings(ctx context.Context, tx store.Tx, id domain.LessonID) (bool, error) {
	bookings, err := tx.Bookings().ListByLesson(ctx, id)

	return len(bookings) > 0, err
}

func (d ClassData) class(id domain.ClassID, term domain.TermID) domain.Class {
	return domain.Class{
		ID: id, TermID: term, CourseTypeID: d.CourseTypeID, RoomID: d.RoomID, InstructorID: d.InstructorID,
		AccompanistID: d.AccompanistID, Level: d.Level, Weekday: d.Weekday, StartTime: d.StartTime,
		Duration: d.Duration, Capacity: d.Capacity, Retired: d.Retired,
	}
}
